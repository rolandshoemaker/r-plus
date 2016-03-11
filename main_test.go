package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-github/github"
)

type testAPI struct {
	hits map[string]string
	t    *testing.T
}

func (ta *testAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		ta.t.Fatalf("Failed to read request body: %s", err)
	}
	var status github.StatusEvent
	err = json.Unmarshal(body, &status)
	if err != nil {
		ta.t.Fatalf("Failed  to unmarshal status event: %s", err)
	}
	ta.hits[r.URL.Path] = *status.State
}

func TestMapMethods(t *testing.T) {
	ta := &testAPI{make(map[string]string), t}
	serv := httptest.NewServer(ta)
	defer serv.Close()
	apiBase = serv.URL

	rp := &rplus{
		pending:   make(map[int]*pull),
		client:    new(http.Client),
		repo:      "testing/repo",
		reviewers: map[string]struct{}{"rolandshoemaker": struct{}{}},
	}

	rp.newCommit(10, "hash", "roland")
	if rp.pending[10] == nil {
		t.Fatal("newCommit didn't add entry")
	}
	if rp.pending[10].currentHash != "hash" {
		t.Fatalf("newCommit added entry with incorrect hash: %s", rp.pending[10].currentHash)
	}
	if rp.pending[10].reviews != 0 {
		t.Fatalf("newCommit added entry with non-zero reviews: %d", rp.pending[10].reviews)
	}
	if ta.hits["/repos/testing/repo/statuses/hash"] == "" {
		t.Fatal("newCommit didn't send pending status")
	}
	if ta.hits["/repos/testing/repo/statuses/hash"] != "pending" {
		t.Fatalf("newCommit sent incorrect status: %s", ta.hits["hash"])
	}

	rp.newPlus(10, "rolandshoemaker")
	if ta.hits["/repos/testing/repo/statuses/hash"] != "success" {
		t.Fatalf("newPlus sent incorrect status: %s", ta.hits["hash"])
	}
	if rp.pending[10] != nil {
		t.Fatal("newPlus should've removed pending pull after successful status was pushed")
	}

	rp.requiredReviews = 2
	rp.newCommit(1, "other-hash", "roland")
	if rp.pending[1] == nil {
		t.Fatal("newCommit didn't add entry")
	}
	if rp.pending[1].currentHash != "other-hash" {
		t.Fatalf("newCommit added entry with incorrect hash: %s", rp.pending[1].currentHash)
	}
	rp.newPlus(1, "rolandshoemaker")
	if rp.pending[1] == nil {
		t.Fatal("newPlus removed an entry when it shouldn't have")
	}
	if rp.pending[1].reviews != 1 {
		t.Fatalf("newPlus didn't increment number of reviews: %d", rp.pending[1].reviews)
	}
	if ta.hits["/repos/testing/repo/statuses/other-hash"] != "pending" {
		t.Fatalf("newPlus change status when it shouldn't: %s", ta.hits["hash"])
	}

	rp.newPlus(12, "rolandshoemaker")
	if rp.pending[12] != nil {
		t.Fatal("newPlus acted on a nil pull")
	}
}

func TestVerifiedHandler(t *testing.T) {
	rp := &rplus{secret: []byte("secret")}
	success := false
	h := rp.verifiedHandler(func(b []byte, w http.ResponseWriter) {
		success = true
	})
	rec := httptest.NewRecorder()
	body := "hi thar!"
	req := &http.Request{
		Body:   ioutil.NopCloser(strings.NewReader(body)),
		Method: "POST",
		Header: make(map[string][]string),
	}
	mac := hmac.New(sha1.New, rp.secret)
	mac.Write([]byte(body))
	expectedMAC := mac.Sum(nil)
	req.Header.Add("X-Hub-Signature", fmt.Sprintf("sha1=%X", expectedMAC))
	h(rec, req)
	if !success {
		t.Fatal("Failed to verify signature")
	}
}

func TestServer(t *testing.T) {
	ta := &testAPI{make(map[string]string), t}
	serv := httptest.NewServer(ta)
	defer serv.Close()
	apiBase = serv.URL

	rp := &rplus{
		pending:         make(map[int]*pull),
		client:          new(http.Client),
		repo:            "testing/repo",
		reviewers:       map[string]struct{}{"rolandshoemaker": struct{}{}},
		requiredReviews: 1,
		reviewPattern:   regexp.MustCompile(`r\+`),
	}

	rec := httptest.NewRecorder()

	roland := "roland"
	user := &github.User{Login: &roland}
	action := "opened"
	num := 1
	sha := "hash"
	pr := &github.PullRequest{Head: &github.PullRequestBranch{SHA: &sha}, User: user}
	prEvent := github.PullRequestEvent{
		Action:      &action,
		Number:      &num,
		PullRequest: pr,
	}
	body, err := json.Marshal(prEvent)
	if err != nil {
		t.Fatalf("Failed to marshal PullRequestEvent: %s", err)
	}
	rp.prHandler(body, rec)
	if rp.pending[1] == nil {
		t.Fatal("entry wasn't added to pending map")
	}
	if rp.pending[1].currentHash != "hash" {
		t.Fatalf("entry has incorrect hash: %s", rp.pending[1].currentHash)
	}
	if rp.pending[1].reviews != 0 {
		t.Fatalf("entry has non-zero reviews: %d", rp.pending[1].reviews)
	}
	if ta.hits["/repos/testing/repo/statuses/hash"] != "pending" {
		t.Fatalf("incorrect status sent for entry: %s", ta.hits["hash"])
	}

	// incorrect action
	action = "closed"
	num = 2
	body, err = json.Marshal(prEvent)
	if err != nil {
		t.Fatalf("Failed to marshal PullRequestEvent: %s", err)
	}
	rp.prHandler(body, rec)
	if rp.pending[2] != nil {
		t.Fatal("entry was added to pending map")
	}

	// okay action, should overwrite previous hash
	action = "synchronize"
	sha = "better-hash"
	num = 1
	body, err = json.Marshal(prEvent)
	if err != nil {
		t.Fatalf("Failed to marshal PullRequestEvent: %s", err)
	}
	rp.prHandler(body, rec)
	if rp.pending[1] == nil {
		t.Fatal("entry wasn't added to pending map")
	}
	if rp.pending[1].currentHash != "better-hash" {
		t.Fatalf("entry has incorrect hash: %s", rp.pending[1].currentHash)
	}
	if rp.pending[1].reviews != 0 {
		t.Fatalf("entry has non-zero reviews: %d", rp.pending[1].reviews)
	}
	if ta.hits["/repos/testing/repo/statuses/better-hash"] != "pending" {
		t.Fatalf("incorrect status sent for entry: %s", ta.hits["better-hash"])
	}

	issue := &github.Issue{PullRequestLinks: &github.PullRequestLinks{}, Number: &num}
	commentBody := "r+"
	roland = "rolandshoemaker"
	comment := &github.IssueComment{Body: &commentBody}
	issueEvent := github.IssueCommentEvent{
		Issue:   issue,
		Comment: comment,
		Sender:  user,
	}
	body, err = json.Marshal(issueEvent)
	if err != nil {
		t.Fatalf("Failed to marshal IssueCommentEvent: %s", err)
	}
	rp.commentHandler(body, rec)
	if ta.hits["/repos/testing/repo/statuses/better-hash"] != "success" {
		t.Fatalf("newPlus sent incorrect status: %s", ta.hits["hash"])
	}
	if rp.pending[1] != nil {
		t.Fatal("newPlus should've removed pending pull after successful status was pushed")
	}
}
