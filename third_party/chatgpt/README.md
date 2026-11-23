# ChatGPT sign-in references and brand asset

FreeMD implements the public OAuth / OpenID Connect and Responses protocols
independently in Go. It does not embed, translate or redistribute the DevKit's
noncommercial SDK, React components or example application code.

- Protocol and UI documentation: <https://developers.openai.com/siwc>
- Reference repository: <https://github.com/openai/sign-in-with-chatgpt-devkit>
- Reviewed revision: `0a36fefeb913055c8c7a1a29b63d82b96b2e841a` (2026-11-23).

The unmodified `assets/brand/chatgpt-button-black-21.svg` from that revision is
included as `internal/app/assets/chatgpt/mark.svg` solely to identify the ChatGPT
sign-in action and connected account. SHA-256:
`b8b9aba7ba591f54bf1805f422bc23f32c44ee993643cc378cc77f766061f7a1`.

OpenAI owns the ChatGPT mark. It is excluded from the DevKit's code license and
from FreeMD's MIT license. Its use follows the
[Sign in with ChatGPT UI guidelines](https://developers.openai.com/siwc/ui-ux-guidelines)
and [OpenAI brand guidelines](https://openai.com/brand/). No sponsorship or
endorsement is claimed. No bundled fonts or other DevKit assets are included.

OIDC verification uses `github.com/coreos/go-oidc/v3` and
`github.com/go-jose/go-jose/v4`, both Apache-2.0; their license copies are in
this directory. The transitive `golang.org/x/oauth2` dependency is BSD-3-Clause.
Pinned versions and checksums are in `../manifests/go-mod.json`.
