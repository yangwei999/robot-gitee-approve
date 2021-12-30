package main

import (
	"fmt"

	"github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/robot-gitee-framework"
	sdk "github.com/opensourceways/go-gitee/gitee"
	"github.com/opensourceways/repo-owners-cache/grpc/client"
	"github.com/sirupsen/logrus"
)

const botName = "approve"

type iClient interface {
	GetPullRequestChanges(org, repo string, number int32) ([]sdk.PullRequestFiles, error)
	GetPRLabels(org, repo string, number int32) ([]sdk.Label, error)
	ListPRComments(org, repo string, number int32) ([]sdk.PullRequestComments, error)
	DeletePRComment(org, repo string, ID int32) error
	CreatePRComment(org, repo string, number int32, comment string) error
	GetBot() (sdk.User, error)
	AddPRLabel(org, repo string, number int32, label string) error
	RemovePRLabel(org, repo string, number int32, label string) error
}

func newRobot(cli iClient, cacheCli *client.Client, botName string) *robot {
	return &robot{cli: ghclient{cli}, cacheCli: cacheCli, botName: botName}
}

type robot struct {
	cacheCli *client.Client
	cli      ghclient
	botName  string
}

func (bot *robot) NewConfig() config.Config {
	return &configuration{}
}

func (bot *robot) getConfig(cfg config.Config, org, repo string) (*botConfig, error) {
	c, ok := cfg.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}

	if bc := c.configFor(org, repo); bc != nil {
		return bc, nil
	}

	return nil, fmt.Errorf("no config for this repo:%s/%s", org, repo)
}

func (bot *robot) RegisterEventHandler(f framework.HandlerRegitster) {
	f.RegisterPullRequestHandler(bot.handlePREvent)
	f.RegisterNoteEventHandler(bot.handleNoteEvent)
}

func (bot *robot) handlePREvent(e *sdk.PullRequestEvent, c config.Config, log *logrus.Entry) error {
	action := sdk.GetPullRequestAction(e)
	if !(action == sdk.ActionOpen || action == sdk.PRActionChangedSourceBranch) {
		return nil
	}

	org, repo := e.GetOrgRepo()

	cfg, err := bot.getConfig(c, org, repo)
	if err != nil {
		return err
	}

	return bot.handle(org, repo, e.GetPullRequest(), cfg, log)
}

func (bot *robot) handleNoteEvent(e *sdk.NoteEvent, c config.Config, log *logrus.Entry) error {
	if !e.IsCreatingCommentEvent() || !e.IsPullRequest() {
		return nil
	}

	org, repo := e.GetOrgRepo()

	cfg, err := bot.getConfig(c, org, repo)
	if err != nil {
		return err
	}

	if bot.botName == e.GetCommenter() || !isApproveCommand(e.GetComment().GetBody(), false) {
		return nil
	}

	return bot.handle(org, repo, e.GetPullRequest(), cfg, log)
}
