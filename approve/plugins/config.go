/*
Copyright 2018 The Kubernetes Authors.

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

package plugins

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Approve specifies a configuration for a single approve.
//
// The configuration for the approve plugin is defined as a list of these structures.
type Approve struct {
	// Repos is either of the form org/repos or just org.
	Repos []string `json:"repos,omitempty"`
	// IssueRequired indicates if an associated issue is required for approval in
	// the specified repos.
	IssueRequired bool `json:"issue_required,omitempty"`

	// TODO(fejta): delete in June 2019
	DeprecatedImplicitSelfApprove *bool `json:"implicit_self_approve,omitempty"`
	// RequireSelfApproval requires PR authors to explicitly approve their PRs.
	// Otherwise the plugin assumes the author of the PR approves the changes in the PR.
	RequireSelfApproval *bool `json:"require_self_approval,omitempty"`

	// LgtmActsAsApprove indicates that the lgtm command should be used to
	// indicate approval
	LgtmActsAsApprove bool `json:"lgtm_acts_as_approve,omitempty"`

	// ReviewActsAsApprove should be replaced with its non-deprecated inverse: ignore_review_state.
	// TODO(fejta): delete in June 2019
	DeprecatedReviewActsAsApprove *bool `json:"review_acts_as_approve,omitempty"`
	// IgnoreReviewState causes the approve plugin to ignore the GitHub review state. Otherwise:
	// * an APPROVE github review is equivalent to leaving an "/approve" message.
	// * A REQUEST_CHANGES github review is equivalent to leaving an /approve cancel" message.
	IgnoreReviewState *bool `json:"ignore_review_state,omitempty"`
}

var (
	warnImplicitSelfApprove time.Time
	warnReviewActsAsApprove time.Time
)

func (a Approve) HasSelfApproval() bool {
	if a.DeprecatedImplicitSelfApprove != nil {
		warnDeprecated(&warnImplicitSelfApprove, 5*time.Minute, "Please update plugins.yaml to use require_self_approval instead of the deprecated implicit_self_approve before June 2019")
		return *a.DeprecatedImplicitSelfApprove
	} else if a.RequireSelfApproval != nil {
		return !*a.RequireSelfApproval
	}
	return true
}

func (a Approve) ConsiderReviewState() bool {
	if a.DeprecatedReviewActsAsApprove != nil {
		warnDeprecated(&warnReviewActsAsApprove, 5*time.Minute, "Please update plugins.yaml to use ignore_review_state instead of the deprecated review_acts_as_approve before June 2019")
		return *a.DeprecatedReviewActsAsApprove
	} else if a.IgnoreReviewState != nil {
		return !*a.IgnoreReviewState
	}
	return true
}

var warnLock sync.RWMutex // Rare updates and concurrent readers, so reuse the same lock

// warnDeprecated prints a deprecation warning for a particular configuration
// option.
func warnDeprecated(last *time.Time, freq time.Duration, msg string) {
	// have we warned within the last freq?
	warnLock.RLock()
	fresh := time.Now().Sub(*last) <= freq
	warnLock.RUnlock()
	if fresh { // we've warned recently
		return
	}
	// Warning is stale, will we win the race to warn?
	warnLock.Lock()
	defer warnLock.Unlock()
	now := time.Now()           // Recalculate now, we might wait awhile for the lock
	if now.Sub(*last) <= freq { // Nope, we lost
		return
	}
	*last = now
	logrus.Warn(msg)
}
