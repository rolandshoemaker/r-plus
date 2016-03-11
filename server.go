package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/google/go-github/github"
)

func (rp *rplus) verifiedHandler(handler func([]byte, http.ResponseWriter)) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			fmt.Fprintf(os.Stderr, "Invalid request method: %s", r.Method)
			return
		}

		// Get signature
		githubSignature := r.Header.Get("X-Hub-Signature")
		if githubSignature == "" {
			fmt.Fprintln(os.Stderr, "No signature on request")
			return
		}

		// Read body
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading request body: %s", err)
			return
		}

		// Verify signature
		mac := hmac.New(sha1.New, rp.secret)
		mac.Write(body)
		expectedMAC := mac.Sum(nil)
		if len(githubSignature) < 5 {
			fmt.Fprintf(os.Stderr, "Invalid signature on request, no actual signature")
			return
		}
		sigBytes, err := hex.DecodeString(githubSignature[5:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid signature on request: %s", err)
			return
		}
		if match := hmac.Equal(sigBytes, expectedMAC); !match {
			fmt.Fprintf(os.Stderr, "Invalid signature on request, provided: %x, expected: sha1=%x", githubSignature, expectedMAC)
			return
		}

		fmt.Fprintf(os.Stdout, "Request with valid signature for endpoint: %s", r.URL)
		handler(body, w)
	})
}

func (rp *rplus) prHandler(body []byte, w http.ResponseWriter) {
	var event github.PullRequestEvent
	err := json.Unmarshal(body, &event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unmarshal PR event: %s", err)
		return
	}
	if *event.Action != "opened" && *event.Action != "synchronize" {
		return
	}
	rp.newCommit(*event.Number, *event.PullRequest.Head.SHA)
}

func (rp *rplus) commentHandler(body []byte, w http.ResponseWriter) {
	var event github.IssueCommentEvent
	err := json.Unmarshal(body, &event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unmarshal comment event: %s", err)
		return
	}
	if event.Issue.PullRequestLinks == nil {
		return
	}
	if _, present := rp.reviewers[*event.Sender.Login]; !present {
		return
	}
	if !rp.reviewPattern.Match([]byte(*event.Comment.Body)) {
		return
	}
	rp.newPlus(*event.Issue.Number)
}
