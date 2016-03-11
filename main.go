package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v2"
)

type pull struct {
	currentHash string
	author      string
	reviews     int
}

type rplus struct {
	requiredReviews int
	reviewers       map[string]struct{}
	reviewPattern   *regexp.Regexp
	selfReview      bool
	repo            string // username/project
	secret          []byte

	pending map[int]*pull
	pMu     sync.Mutex

	client *http.Client
}

func (rp *rplus) newCommit(pr int, hash, author string) {
	rp.pMu.Lock()
	defer rp.pMu.Unlock()
	rp.pending[pr] = &pull{currentHash: hash, author: author}
	err := rp.updateStatus(hash, "pending")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to update status for commit '%s' on #%d: %s\n", hash, pr, err)
	}
}

func (rp *rplus) newPlus(pr int, reviewer string) {
	if _, present := rp.reviewers[reviewer]; !present {
		return
	}
	rp.pMu.Lock()
	defer rp.pMu.Unlock()
	if _, present := rp.pending[pr]; !present {
		fmt.Fprintf(os.Stderr, "Received r+ on PR I don't know about: #%d\n", pr)
		return
	}
	o := rp.pending[pr]
	if !rp.selfReview && o.author == reviewer {
		return
	}
	o.reviews++
	if o.reviews >= rp.requiredReviews {
		err := rp.updateStatus(o.currentHash, "success")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to update status for commit '%s' on #%d: %s\n", o.currentHash, pr, err)
			return
		}
		delete(rp.pending, pr)
	}
}

var (
	apiBase    = "https://api.github.com"
	statusDesc = ""
	statusCtx  = "github/reviews"
)

func (rp *rplus) updateStatus(hash string, state string) error {
	status := github.StatusEvent{
		State:       &state,
		Description: &statusDesc,
		Context:     &statusCtx,
	}
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/repos/%s/statuses/%s", apiBase, rp.repo, hash), // XXX
		bytes.NewBuffer(data),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := rp.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		return nil
	}
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return fmt.Errorf("unexpected response status code, body: %s", strings.Replace(string(content), "\n", "", -1))
}

func (rp *rplus) run(webhookAddr, certPath, keyPath, prPath, commentPath string) error {
	http.HandleFunc(prPath, rp.verifiedHandler(rp.prHandler))
	http.HandleFunc(commentPath, rp.verifiedHandler(rp.commentHandler))
	if certPath != "" && keyPath != "" {
		return http.ListenAndServeTLS(webhookAddr, certPath, keyPath, nil)
	}
	return http.ListenAndServe(webhookAddr, nil)
}

type config struct {
	Reviewers       []string
	RequiredReviews int    `yaml:"required-reviews"`
	ReviewPattern   string `yaml:"review-pattern"`
	SelfReview      bool   `yaml:"self-review"`
	Repo            string `yaml:"repo"`
	AccessToken     string `yaml:"access-token"`
	WebhookServer   struct {
		Addr        string `yaml:"addr"`
		Cert        string `yaml:"certificate"`
		CertKey     string `yaml:"certificate-key"`
		PRPath      string `yaml:"pr-path"`
		CommentPath string `yaml:"comment-path"`
		Secret      string `yaml:"secret"`
	} `yaml:"webhook-server"`
}

func main() {
	configPath := flag.String("config", "config.yml", "Path to configuration file")
	flag.Parse()

	contents, err := ioutil.ReadFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read config file '%s': %s\n", *configPath, err)
		return
	}
	var c config
	err = yaml.Unmarshal(contents, &c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse config file '%s': %s\n", *configPath, err)
		return
	}
	reviewerMap := make(map[string]struct{}, len(c.Reviewers))
	for _, r := range c.Reviewers {
		reviewerMap[r] = struct{}{}
	}
	reviewPattern, err := regexp.Compile(c.ReviewPattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to compile review pattern: %s\n", err)
		return
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: c.AccessToken})
	tc := oauth2.NewClient(oauth2.NoContext, ts)

	rp := &rplus{
		requiredReviews: c.RequiredReviews,
		reviewers:       reviewerMap,
		reviewPattern:   reviewPattern,
		selfReview:      c.SelfReview,
		repo:            c.Repo,
		secret:          []byte(c.WebhookServer.Secret),
		pending:         make(map[int]*pull),
		client:          tc,
	}
	err = rp.run(
		c.WebhookServer.Addr,
		c.WebhookServer.Cert,
		c.WebhookServer.CertKey,
		c.WebhookServer.PRPath,
		c.WebhookServer.CommentPath,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run r-plus: %s\n", err)
	}
}
