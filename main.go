package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sync"

	"github.com/google/go-github/github"
)

type pull struct {
	currentHash string
	reviews     int
}

type rplus struct {
	requiredReviews int
	reviewers       map[string]struct{}
	reviewPattern   regexp.Regexp
	repo            string // username/project
	secret          []byte

	pending map[int]*pull
	pMu     sync.Mutex

	client *http.Client
}

func new(repo string, requiredReviews int, reviewers []string, secret []byte) *rplus {
	reviewerMap := make(map[string]struct{}, len(reviewers))
	for _, r := range reviewers {
		reviewerMap[r] = struct{}{}
	}
	return &rplus{
		repo:            repo,
		requiredReviews: requiredReviews,
		reviewers:       reviewerMap,
		pending:         make(map[int]*pull),
		secret:          secret,
	}
}

func (rp *rplus) newCommit(pr int, hash string) {
	rp.pMu.Lock()
	defer rp.pMu.Unlock()
	rp.pending[pr] = &pull{currentHash: hash}
	err := rp.updateStatus(hash, "pending")
	if err != nil {
		// log
	}
}

func (rp *rplus) newPlus(pr int) {
	rp.pMu.Lock()
	defer rp.pMu.Unlock()
	if _, present := rp.pending[pr]; !present {
		// log
		return
	}
	o := rp.pending[pr]
	o.reviews++
	if o.reviews >= rp.requiredReviews {
		err := rp.updateStatus(o.currentHash, "success")
		if err != nil {
			// log
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
		// log
		return err
	}
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/repos/%s/statuses/%s", apiBase, rp.repo, hash), // XXX
		bytes.NewBuffer(data),
	)
	if err != nil {
		// log
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := rp.client.Do(req)
	if err != nil {
		// log
		return err
	}
	defer resp.Body.Close()
	// content, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	//   log
	//   return err
	// }
	// if resp.StatusCode > 201 {
	//   log unexpected status + body
	//   return fmt.Errorf("")
	// }
	return nil
}

func main() {

}
