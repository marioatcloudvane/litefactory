# dotnet-web 1.0.0 compatibility

Release-time metadata reviewed 2026-10-10. Compatibility policy and package proof
are separate from executed application compatibility; this release supplies no
sample application and no live provider demonstration.

| Item | Supported release baseline |
| --- | --- |
| .NET SDK | Exactly 10.0.401, stable, released 2026-09-08 |
| .NET runtime | 10.0.12 |
| ASP.NET Core / Blazor shared framework | 10.0.12 (10 family) |
| Target framework | net10.0 |
| C# | SDK default language selection; no preview or forced LangVersion |
| Restore/build/format/test CLI | Tools bundled with SDK 10.0.401; no floating tool resolution |
| Auxiliary tools/packages enabled by team | None; no global or local tools installed by these recipes |
| Test packages | Preserve explicit repository pins and adapter/framework choices; MTP mode requires MTP 1.7 or later, with a concrete stable repository pin |
| Factory hosts | Admitted macOS or Linux, arm64 or amd64; POSIX sh and prepared dotnet on operator PATH |

The operator must prepare the exact SDK and matching runtimes on a supported host
before admission/checks. The bounded supported host set for this team is macOS 15 or 26, or Ubuntu
24.04, on arm64 or amd64, with the OS publisher's supported native prerequisites.
Other distributions are not covered by this initial matrix. Linux native execution
requires glibc 2.27 or newer; QEMU-emulated hosts are not supported. A checksummed profile does not
admit a host or grant network/filesystem access. global.json selects the SDK and
runner; pins and roll-forward policy must resolve to this exact SDK. Recipes refuse
a different selected SDK, even when newer SDKs are installed. Do not rewrite
SDK selection, NuGet configuration, package pins, lockfiles or local tool manifests
as an automatic check fix. No universal Node.js, Visual Studio or MAUI requirement.

WebAssembly repositories may require the SDK-matched wasm-tools workload and,
for AOT, native toolchain prerequisites appropriate to the admitted host. Browser
checks require the repository's pinned browser tooling and prepared browsers.
Neither is universally enabled. Operator preparation must precede use: never
install workloads, browsers, development certificates or global tools automatically.
Local tool manifests remain repository-owned; these recipes use bundled CLI tools
and do not restore unrelated local tools. Add an explicitly admitted, pinned recipe
only when the repository's checks need those tools.

The operator must admit CLI-home/NuGet cache locations through existing isolation
settings; the profile does not set HOME, PATH, DOTNET_CLI_HOME or NUGET_PACKAGES.
Only approved non-secret environment values are propagated. Use repository-local
bin, obj and test outputs within admitted writable paths. Configured package cache
paths and network feeds must also be admitted. If cache, network or output authority
is missing, report failure; do not silently redirect outside the sandbox or skip.

Sources retained as informational references; all execution-critical instructions
are bundled here:
- [Microsoft .NET 10 release metadata](https://builds.dotnet.microsoft.com/dotnet/release-metadata/10.0/releases.json)
- [Supported operating systems](https://github.com/dotnet/core/blob/main/release-notes/10.0/supported-os.md)
- [SDK selection](https://learn.microsoft.com/en-us/dotnet/core/tools/global-json)
- [Render modes](https://learn.microsoft.com/en-us/aspnet/core/blazor/components/render-modes?view=aspnetcore-10.0)
- [MTP dotnet test contract](https://learn.microsoft.com/en-us/dotnet/core/tools/dotnet-test-mtp)
- [VSTest zero-test behavior](https://github.com/microsoft/vstest/blob/main/docs/commandline.md)
