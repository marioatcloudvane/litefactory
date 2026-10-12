# Attachments

[Documentation](../README.md) · [Using the factory](README.md)

Attach images or documents to the issue or to the comment that asks for the work. The agent can then use their contents.

Upload a screenshot through GitHub and mention it in your request:

```text
/research Explain why the uploaded screenshot differs from the current settings page.
```

## What is picked up

- Only GitHub upload links are looked at: `github.com/user-attachments/assets/...` and the `objects`, `user-images` and `private-user-images` GitHub hosts, over HTTPS. Other links are ignored and never fetched.
- The comment that triggered the run is read first, then the issue body. A link that appears twice counts once.
- Supported files are PNG, JPEG, GIF, WebP, PDF, DOCX and PPTX. The factory checks the file's actual contents, not its name, so a ZIP renamed to `.docx` is not accepted.
- The coding-agent permission profile for the run must allow attachments (`attachments: true` in `tool_profiles`; see [permissions](../configuration/permissions.md)).

| Default limit | Value |
| --- | --- |
| One image | 5 MiB |
| One document | 10 MiB |
| Total per run | 25 MiB |
| Files per run | 10 |

You can change the limits in [config.yaml](../configuration/config-yaml.md).

Accepted files are saved locally and listed in the agent's prompt. Documents stay as raw files; whether the agent can read a PDF or Office file depends on the tools it has. A file being accepted does not guarantee it is understood.

## When a file is refused

The report says why: too large, unsupported type, could not be fetched, too many files, or the total size is used up. The other files are still used, and one refused file does not fail the run. Reduce the size or number, use a supported format, or fix access, then send a new request. If the missing file is essential, say so in the request.

See [permissions](../configuration/permissions.md), [working with issues](working-with-issues.md) and [troubleshooting](../operations/troubleshooting.md).
