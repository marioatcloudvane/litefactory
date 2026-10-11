# Attachments

Attach supporting images or documents to the issue or triggering comment so the agent can use their contents. The run must permit attachment ingestion and the factory must have an asset fetcher and access to the uploaded asset.

Upload a screenshot through GitHub, then include it in the request:

```text
/research Explain why the uploaded screenshot differs from the current settings page.
```

Only recognized HTTPS GitHub asset URLs are scanned: `github.com/user-attachments/assets/` and the objects, user-images and private-user-images GitHub asset hosts. Ordinary external links are ignored. The trigger comment is scanned before the issue body; duplicate URLs count once. Supported bytes are PNG, JPEG, GIF, WebP, PDF, DOCX and PPTX. Classification uses file contents; renaming an arbitrary ZIP to DOCX does not make it supported. DOCX/PPTX containers are identified by Office parts.

| Default limit | Value |
| --- | --- |
| One image | 5 MiB |
| One document | 10 MiB |
| Total accepted bytes per run | 25 MiB |
| Accepted files per run | 10 |

Limits are configurable in the [YAML reference](../configuration/config-yaml.md). Accepted files are landed locally and described in the agent prompt; the run has an attachment manifest. Documents remain raw files: interpreting or converting them requires tools available to that agent. Successful ingestion does not guarantee successful interpretation.

Declines distinguish oversized files, unsupported types, fetch failures, count exhaustion and total-byte exhaustion. Other attachments can still ingest; a rejected file alone does not fail the run. Check the report and local logs, reduce size/count, use a supported format or repair access, then send a new request. State when the missing file is essential to the answer.

See [permissions](../configuration/permissions.md), [working with issues](working-with-issues.md) and [troubleshooting](../operations/troubleshooting.md).

[Using the factory](README.md)
