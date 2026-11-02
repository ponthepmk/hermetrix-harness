# MacBook and PC provider configuration

The M3/M4 MacBook runs the darwin/arm64 application. The PC runs the Windows application and hosts Bonsai as a separate model provider. Neither application startup nor ordinary model-independent work requires Bonsai to be installed on the Mac. Cloud and other local providers remain independent profiles.

On the Mac, configure the PC provider with its approved authenticated endpoint, exact model and qualified context. Non-loopback endpoints are remote for egress purposes even on a home LAN or tailnet; a private IP is not a local-processing guarantee. The application requires explicit remote_allowed consent before sending project content. Store credentials in the local protected vault, never in endpoint URLs or exported packages.

Current local/remote classification derives from the endpoint. Consequently, do not label an SSH-forwarded loopback endpoint as local inference on the Mac: the model still runs on the PC. Prefer an explicitly configured authenticated remote endpoint for that provider until tunnel-origin metadata is supported. No firewall, bind address, TLS or production model service configuration was changed by this work.

An approved Mac-to-PC endpoint has not been supplied or exercised. Required acceptance: remote consent/refusal, streaming/tool calls, cancel/reconcile behavior, model context/VRAM evidence on PC, and independent Mac application behavior while PC provider is unavailable. The browser microphone and native Mac speech belong to the Mac and are independent of Bonsai model routing.
