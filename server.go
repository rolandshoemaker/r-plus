package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/google/go-github/github"
)

func (rp *rplus) verifiedHandler(handler func([]byte, http.ResponseWriter)) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			// fmt.Printf("[GHWH-SRV] ERROR invalid request method: %s\n", r.Method)
			return
		}

		// Get signature
		githubSignature := r.Header.Get("X-Hub-Signature")
		if githubSignature == "" {
			// fmt.Println("[GHWH-SRV] ERROR no signature on request")
			return
		}

		// Read body
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			// fmt.Printf("[GHWH-SRV] ERROR reading request body, %s\n", err)
			return
		}

		// Verify signature
		mac := hmac.New(sha1.New, rp.secret)
		mac.Write(body)
		expectedMAC := mac.Sum(nil)
		if len(githubSignature) < 5 {
			// fmt.Println("[GHWH-SRV] ERROR invalid signature on request, no actual signature")
			return
		}
		sigBytes, err := hex.DecodeString(githubSignature[5:])
		if err != nil {
			// fmt.Printf("[GHWH-SRV] ERROR invalid signature on request, %s", err)
			return
		}
		if match := hmac.Equal(sigBytes, expectedMAC); !match {
			// fmt.Printf("[GHWH-SRV] ERROR invalid signature on request, provided: %x, expected: sha1=%x", githubSignature, expectedMAC)
			return
		}

		// fmt.Printf("[GHWH-SRV] Request with valid signature, endpoint: %s\n", r.URL)
		handler(body, w)
	})
}

func (rp *rplus) prHandler(body []byte, w http.ResponseWriter) {
	var event github.PullRequestEvent
	err := json.Unmarshal(body, &event)
	if err != nil {
		// log
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
		// log
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
