/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package approve

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/labels"

	"github.com/opensourceways/robot-gitee-approve/approve/approvers"
	"github.com/opensourceways/robot-gitee-approve/approve/config"
	"github.com/opensourceways/robot-gitee-approve/approve/plugins"
)

const (
	approveCommand  = "APPROVE"
	cancelArgument  = "cancel"
	lgtmCommand     = "LGTM"
	noIssueArgument = "no-issue"
)

var (
	associatedIssueRegexFormat = `(?:%s/[^/]+/issues/|#)(\d+)`
	commandRegex               = regexp.MustCompile(`(?m)^/([^\s]+)[\t ]*([^\n\r]*)`)
	notificationRegex          = regexp.MustCompile(`(?is)^\[` + approvers.ApprovalNotificationName + `\] *?([^\n]*)(?:\n\n(.*))?`)

	// deprecatedBotNames are the names of the bots that previously handled approvals.
	// Each can be removed once every PR approved by the old bot has been merged or unapproved.
	deprecatedBotNames = []string{"k8s-merge-robot", "openshift-merge-robot"}

	// handleFunc is used to allow mocking out the behavior of 'handle' while testing.
	handleFunc = handle
)

type githubClient interface {
	GetPullRequest(org, repo string, number int) (*github.PullRequest, error)
	GetPullRequestChanges(org, repo string, number int) ([]github.PullRequestChange, error)
	GetIssueLabels(org, repo string, number int) ([]github.Label, error)
	ListIssueComments(org, repo string, number int) ([]github.IssueComment, error)
	ListReviews(org, repo string, number int) ([]github.Review, error)
	ListPullRequestComments(org, repo string, number int) ([]github.ReviewComment, error)
	DeleteComment(org, repo string, ID int) error
	CreateComment(org, repo string, number int, comment string) error
	BotName() (string, error)
	AddLabel(org, repo string, number int, label string) error
	RemoveLabel(org, repo string, number int, label string) error
	ListIssueEvents(org, repo string, num int) ([]github.ListedIssueEvent, error)
}

type state struct {
	org    string
	repo   string
	branch string
	number int

	body      string
	author    string
	assignees []github.User
	htmlURL   string
}

// Returns associated issue, or 0 if it can't find any.
// This is really simple, and could be improved later.
func findAssociatedIssue(body, org string) (int, error) {
	associatedIssueRegex, err := regexp.Compile(fmt.Sprintf(associatedIssueRegexFormat, org))
	if err != nil {
		return 0, err
	}
	match := associatedIssueRegex.FindStringSubmatch(body)
	if len(match) == 0 {
		return 0, nil
	}
	v, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, err
	}
	return v, nil
}

// handle is the workhorse the will actually make updates to the PR.
// The algorithm goes as:
// - Initially, we build an approverSet
//   - Go through all comments in order of creation.
//     - (Issue/PR comments, PR review comments, and PR review bodies are considered as comments)
//   - If anyone said "/approve", add them to approverSet.
//   - If anyone said "/lgtm" AND LgtmActsAsApprove is enabled, add them to approverSet.
//   - If anyone created an approved review AND ReviewActsAsApprove is enabled, add them to approverSet.
// - Then, for each file, we see if any approver of this file is in approverSet and keep track of files without approval
//   - An approver of a file is defined as:
//     - Someone listed as an "approver" in an OWNERS file in the files directory OR
//     - in one of the file's parent directories
// - Iff all files have been approved, the bot will add the "approved" label.
// - Iff a cancel command is found, that reviewer will be removed from the approverSet
// 	and the munger will remove the approved label if it has been applied
func handle(log *logrus.Entry, ghc githubClient, repo approvers.Repo, githubConfig config.GitHubOptions, opts *plugins.Approve, pr *state) error {
	funcStart := time.Now()
	defer func() {
		log.WithField("duration", time.Since(funcStart).String()).Debug("Completed handle")
	}()
	fetchErr := func(context string, err error) error {
		return fmt.Errorf("failed to get %s for %s/%s#%d: %v", context, pr.org, pr.repo, pr.number, err)
	}

	start := time.Now()
	changes, err := ghc.GetPullRequestChanges(pr.org, pr.repo, pr.number)
	if err != nil {
		return fetchErr("PR file changes", err)
	}
	var filenames []string
	for _, change := range changes {
		filenames = append(filenames, change.Filename)
	}
	issueLabels, err := ghc.GetIssueLabels(pr.org, pr.repo, pr.number)
	if err != nil {
		return fetchErr("issue labels", err)
	}
	hasApprovedLabel := false
	for _, label := range issueLabels {
		if label.Name == labels.Approved {
			hasApprovedLabel = true
			break
		}
	}
	botName, err := ghc.BotName()
	if err != nil {
		return fetchErr("bot name", err)
	}
	issueComments, err := ghc.ListIssueComments(pr.org, pr.repo, pr.number)
	if err != nil {
		return fetchErr("issue comments", err)
	}
	reviewComments, err := ghc.ListPullRequestComments(pr.org, pr.repo, pr.number)
	if err != nil {
		return fetchErr("review comments", err)
	}
	reviews, err := ghc.ListReviews(pr.org, pr.repo, pr.number)
	if err != nil {
		return fetchErr("reviews", err)
	}
	log.WithField("duration", time.Since(start).String()).Debug("Completed github functions in handle")

	start = time.Now()
	approversHandler := approvers.NewApprovers(
		approvers.NewOwners(
			log,
			filenames,
			repo,
			int64(pr.number),
		),
	)
	approversHandler.AssociatedIssue, err = findAssociatedIssue(pr.body, pr.org)
	if err != nil {
		log.WithError(err).Errorf("Failed to find associated issue from PR body: %v", err)
	}
	approversHandler.RequireIssue = opts.IssueRequired
	approversHandler.ManuallyApproved = humanAddedApproved(ghc, log, pr.org, pr.repo, pr.number, botName, hasApprovedLabel)

	// Author implicitly approves their own PR if config allows it
	if opts.HasSelfApproval() {
		approversHandler.AddAuthorSelfApprover(pr.author, pr.htmlURL+"#", false)
	} else {
		// Treat the author as an assignee, and suggest them if possible
		approversHandler.AddAssignees(pr.author)
	}
	log.WithField("duration", time.Since(start).String()).Debug("Completed configuring approversHandler in handle")

	start = time.Now()
	commentsFromIssueComments := commentsFromIssueComments(issueComments)
	comments := append(commentsFromReviewComments(reviewComments), commentsFromIssueComments...)
	comments = append(comments, commentsFromReviews(reviews)...)
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
	approveComments := filterComments(comments, approvalMatcher(botName, opts.LgtmActsAsApprove, opts.ConsiderReviewState()))
	addApprovers(&approversHandler, approveComments, pr.author, opts.ConsiderReviewState())
	log.WithField("duration", time.Since(start).String()).Debug("Completed filering approval comments in handle")

	for _, user := range pr.assignees {
		approversHandler.AddAssignees(user.Login)
	}

	start = time.Now()
	notifications := filterComments(commentsFromIssueComments, notificationMatcher(botName))
	latestNotification := getLast(notifications)
	commandURL := GetBotCommandLink(pr.htmlURL)
	newMessage := updateNotification(githubConfig.LinkURL, pr.org, pr.repo, pr.branch, commandURL, latestNotification, approversHandler)
	log.WithField("duration", time.Since(start).String()).Debug("Completed getting notifications in handle")
	start = time.Now()
	if newMessage != nil {
		for _, notif := range notifications {
			if err := ghc.DeleteComment(pr.org, pr.repo, notif.ID); err != nil {
				log.WithError(err).Errorf("Failed to delete comment from %s/%s#%d, ID: %d.", pr.org, pr.repo, pr.number, notif.ID)
			}
		}
		if err := ghc.CreateComment(pr.org, pr.repo, pr.number, *newMessage); err != nil {
			log.WithError(err).Errorf("Failed to create comment on %s/%s#%d: %q.", pr.org, pr.repo, pr.number, *newMessage)
		}
	}
	log.WithField("duration", time.Since(start).String()).Debug("Completed adding/deleting approval comments in handle")

	start = time.Now()
	if !approversHandler.IsApproved() {
		if hasApprovedLabel {
			if err := ghc.RemoveLabel(pr.org, pr.repo, pr.number, labels.Approved); err != nil {
				log.WithError(err).Errorf("Failed to remove %q label from %s/%s#%d.", labels.Approved, pr.org, pr.repo, pr.number)
			}
		}
	} else if !hasApprovedLabel {
		if err := ghc.AddLabel(pr.org, pr.repo, pr.number, labels.Approved); err != nil {
			log.WithError(err).Errorf("Failed to add %q label to %s/%s#%d.", labels.Approved, pr.org, pr.repo, pr.number)
		}
	}
	log.WithField("duration", time.Since(start).String()).Debug("Completed adding/deleting approval labels in handle")
	return nil
}

func humanAddedApproved(ghc githubClient, log *logrus.Entry, org, repo string, number int, botName string, hasLabel bool) func() bool {
	findOut := func() bool {
		if !hasLabel {
			return false
		}
		events, err := ghc.ListIssueEvents(org, repo, number)
		if err != nil {
			log.WithError(err).Errorf("Failed to list issue events for %s/%s#%d.", org, repo, number)
			return false
		}
		var lastAdded github.ListedIssueEvent
		for _, event := range events {
			// Only consider "approved" label added events.
			if event.Event != github.IssueActionLabeled || event.Label.Name != labels.Approved {
				continue
			}
			lastAdded = event
		}

		if lastAdded.Actor.Login == "" || lastAdded.Actor.Login == botName || isDeprecatedBot(lastAdded.Actor.Login) {
			return false
		}
		return true
	}

	var cache *bool
	return func() bool {
		if cache == nil {
			val := findOut()
			cache = &val
		}
		return *cache
	}
}

func approvalMatcher(botName string, lgtmActsAsApprove, reviewActsAsApprove bool) func(*comment) bool {
	return func(c *comment) bool {
		return isApprovalCommand(botName, lgtmActsAsApprove, c) || isApprovalState(botName, reviewActsAsApprove, c)
	}
}

func isApprovalCommand(botName string, lgtmActsAsApprove bool, c *comment) bool {
	if c.Author == botName || isDeprecatedBot(c.Author) {
		return false
	}

	for _, match := range commandRegex.FindAllStringSubmatch(c.Body, -1) {
		cmd := strings.ToUpper(match[1])
		if (cmd == lgtmCommand && lgtmActsAsApprove) || cmd == approveCommand {
			return true
		}
	}
	return false
}

func isApprovalState(botName string, reviewActsAsApprove bool, c *comment) bool {
	if c.Author == botName || isDeprecatedBot(c.Author) {
		return false
	}

	// The review webhook returns state as lowercase, while the review API
	// returns state as uppercase. Uppercase the value here so it always
	// matches the constant.
	reviewState := github.ReviewState(strings.ToUpper(string(c.ReviewState)))

	// ReviewStateApproved = /approve
	// ReviewStateChangesRequested = /approve cancel
	// ReviewStateDismissed = remove previous approval or disapproval
	// (Reviews can go from Approved or ChangesRequested to Dismissed
	// state if the Dismiss action is used)
	if reviewActsAsApprove && (reviewState == github.ReviewStateApproved ||
		reviewState == github.ReviewStateChangesRequested ||
		reviewState == github.ReviewStateDismissed) {
		return true
	}
	return false
}

func notificationMatcher(botName string) func(*comment) bool {
	return func(c *comment) bool {
		if c.Author != botName && !isDeprecatedBot(c.Author) {
			return false
		}
		match := notificationRegex.FindStringSubmatch(c.Body)
		return len(match) > 0
	}
}

func updateNotification(linkURL *url.URL, org, repo, branch, commandURL string, latestNotification *comment, approversHandler approvers.Approvers) *string {
	message := approvers.GetMessage(approversHandler, linkURL, org, repo, branch, commandURL)
	if message == nil || (latestNotification != nil && strings.Contains(latestNotification.Body, *message)) {
		return nil
	}
	return message
}

// addApprovers iterates through the list of comments on a PR
// and identifies all of the people that have said /approve and adds
// them to the Approvers.  The function uses the latest approve or cancel comment
// to determine the Users intention. A review in requested changes state is
// considered a cancel.
func addApprovers(approversHandler *approvers.Approvers, approveComments []*comment, author string, reviewActsAsApprove bool) {
	for _, c := range approveComments {
		if c.Author == "" {
			continue
		}

		if reviewActsAsApprove && c.ReviewState == github.ReviewStateApproved {
			approversHandler.AddApprover(
				c.Author,
				c.HTMLURL,
				false,
			)
		}
		if reviewActsAsApprove && c.ReviewState == github.ReviewStateChangesRequested {
			approversHandler.RemoveApprover(c.Author)
		}

		for _, match := range commandRegex.FindAllStringSubmatch(c.Body, -1) {
			name := strings.ToUpper(match[1])
			if name != approveCommand && name != lgtmCommand {
				continue
			}
			args := strings.ToLower(strings.TrimSpace(match[2]))
			if strings.Contains(args, cancelArgument) {
				approversHandler.RemoveApprover(c.Author)
				continue
			}

			if c.Author == author {
				approversHandler.AddAuthorSelfApprover(
					c.Author,
					c.HTMLURL,
					args == noIssueArgument,
				)
			}

			if name == approveCommand {
				approversHandler.AddApprover(
					c.Author,
					c.HTMLURL,
					args == noIssueArgument,
				)
			} else {
				approversHandler.AddLGTMer(
					c.Author,
					c.HTMLURL,
					args == noIssueArgument,
				)
			}

		}
	}
}

type comment struct {
	Body        string
	Author      string
	CreatedAt   time.Time
	HTMLURL     string
	ID          int
	ReviewState github.ReviewState
}

func commentFromIssueComment(ic *github.IssueComment) *comment {
	if ic == nil {
		return nil
	}
	return &comment{
		Body:      ic.Body,
		Author:    ic.User.Login,
		CreatedAt: ic.CreatedAt,
		HTMLURL:   ic.HTMLURL,
		ID:        ic.ID,
	}
}

func commentsFromIssueComments(ics []github.IssueComment) []*comment {
	comments := make([]*comment, 0, len(ics))
	for i := range ics {
		comments = append(comments, commentFromIssueComment(&ics[i]))
	}
	return comments
}

func commentFromReviewComment(rc *github.ReviewComment) *comment {
	if rc == nil {
		return nil
	}
	return &comment{
		Body:      rc.Body,
		Author:    rc.User.Login,
		CreatedAt: rc.CreatedAt,
		HTMLURL:   rc.HTMLURL,
		ID:        rc.ID,
	}
}

func commentsFromReviewComments(rcs []github.ReviewComment) []*comment {
	comments := make([]*comment, 0, len(rcs))
	for i := range rcs {
		comments = append(comments, commentFromReviewComment(&rcs[i]))
	}
	return comments
}

func commentFromReview(review *github.Review) *comment {
	if review == nil {
		return nil
	}
	return &comment{
		Body:        review.Body,
		Author:      review.User.Login,
		CreatedAt:   review.SubmittedAt,
		HTMLURL:     review.HTMLURL,
		ID:          review.ID,
		ReviewState: review.State,
	}
}

func commentsFromReviews(reviews []github.Review) []*comment {
	comments := make([]*comment, 0, len(reviews))
	for i := range reviews {
		comments = append(comments, commentFromReview(&reviews[i]))
	}
	return comments
}

func filterComments(comments []*comment, filter func(*comment) bool) []*comment {
	filtered := make([]*comment, 0, len(comments))
	for _, c := range comments {
		if filter(c) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func getLast(cs []*comment) *comment {
	if len(cs) == 0 {
		return nil
	}
	return cs[len(cs)-1]
}

func isDeprecatedBot(login string) bool {
	for _, deprecated := range deprecatedBotNames {
		if deprecated == login {
			return true
		}
	}
	return false
}
