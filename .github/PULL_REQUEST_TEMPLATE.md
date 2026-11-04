## Summary

Describe the purpose of this pull request and which component(s) it touches (`internal/admission`, `internal/verification`, `internal/registry`, `internal/controller`, `internal/policy`, `charts/`, `terraform/`, or `docs/`).

## Related Issues / ADRs

- Closes #

## Security & Architectural Checklist

- [ ] Preserves zero network/registry I/O on the `ValidatingAdmissionWebhook` path (`internal/admission`).
- [ ] Preserves zero model weight downloads (`MaxMetadataBlobBytes = 16 MiB` enforced).
- [ ] Preserves fail-closed (`failurePolicy: Fail`) admission behavior.
- [ ] Preserves isolation between `modelSigners`, `evaluators`, and `releaseAuthorities` trust domains.
- [ ] Preserves non-self-assignable admission exemptions (`req.UserInfo.Username` verified).
- [ ] Contains no environment-specific GCP project IDs, service account emails, `.tfstate`, `tfplan`, or private keys.

## Verification Performed

- [ ] `make lint` (`gofmt`, `go vet`, `golangci-lint`)
- [ ] `make test` (`go test -race -v ./...`)
- [ ] `make build`
- [ ] `make helm-lint`
- [ ] `make terraform-validate`
