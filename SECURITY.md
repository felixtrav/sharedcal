# Security policy

## Supported versions

Security fixes go into the latest minor release. Container tags `:1` (latest
1.x) and `:1.N` move to the fixed release; pin a digest only if you also follow
releases.

## Reporting a vulnerability

Please **do not open a public issue**. Use GitHub's private vulnerability
reporting ("Security" tab → "Report a vulnerability") on this repository.
Include the version (`sharedcal --version` or the image digest), the
configuration that matters (database, proxy, IdP) and steps to reproduce.

We aim to acknowledge reports within 3 working days and to ship a fix or
mitigation within 30 days for high-severity issues, crediting you in the
release notes unless you prefer otherwise.

## Scope

In scope: the `sharedcal` binary and container image, the web app, the
example deployment files in `deploy/`. Especially interesting:

- authorization bypasses (role × ownership rules in `internal/authz`);
- session, CSRF or OIDC flow weaknesses;
- the attachment pipeline: type confusion, files served so a browser executes
  them, decompression bombs, path traversal;
- `sharedcal import` accepting a crafted archive that writes outside
  `DATA_DIR` or exhausts resources;
- theme uploads that inject CSS or script.

Out of scope: vulnerabilities in your IdP, reverse proxy or database server;
denial of service that needs a valid admin account; missing hardening on a
deployment that ignores the documented proxy/TLS setup.

## Verifying releases

Release images are signed with cosign (keyless, GitHub OIDC) and carry an SPDX
SBOM and SLSA build provenance:

```sh
IMAGE=ghcr.io/felixtrav/sharedcal:1
cosign verify "$IMAGE" \
  --certificate-identity-regexp '^https://github.com/felixtrav/sharedcal/\.github/workflows/ci\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
cosign verify-attestation --type spdxjson "$IMAGE" \
  --certificate-identity-regexp '^https://github.com/felixtrav/sharedcal/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify "oci://$IMAGE" --repo felixtrav/sharedcal
```
