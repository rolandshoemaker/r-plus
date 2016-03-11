# `r-plus`

A simple way to enforce review policies on GitHub pull requests
using [statuses](https://developer.github.com/v3/repos/statuses/)
and [web hooks](https://developer.github.com/webhooks/).

## Configuration

The configuration file uses YAML, most of the fields should be
relatively self-explanatory.

```
reviewers:
  - rolandshoemaker
required-reviews: 1
review-pattern: r\+
repo: rolandshoemaker/r-plus
access-token: oauth-token
webhook-server:
  addr: 0.0.0.0:3344
  cert:
  cert-key:
  pr-path: /wh/pr
  comment-path: /wh/comment
  secret: shhhh
```

The OAuth access token should only require the `status` scope in
order to properly function.

Two webhooks need to be setup, for the `issue_comment` and
`pull_request` event types. In order to reduce headaches they
should be pointing at two different paths but use the same
`secret`.
