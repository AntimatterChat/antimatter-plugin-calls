# Antimatter Calls

![calls_screen](https://user-images.githubusercontent.com/1832946/205749357-1f2d5af3-cfe7-4352-b1f2-953a31d91fca.png)

Calls enables voice calling and screen sharing functionality in Antimatter channels.

## Installation

1. Download the latest version from the [release page](https://github.com/antimatterchat/antimatter-plugin-calls/releases).
2. Upload the file through **System Console > Plugins > Plugin Management**, or manually upload it to the Antimatter server under plugin directory.
3. Configure and enable the plugin.

## Requirements

This plugin demands some network configuration changes to allow audio/video communication between clients, such as opening network ports. Please refer to the [documentation](https://docs.antimatter.example/configure/calls-deployment.html#network) for more details.

## Documentation

[End-user documentation](https://docs.antimatter.example/channels/make-calls.html)
[Calls self-hosted deployment](https://docs.antimatter.example/configure/calls-deployment.html)
[Configuration settings](https://docs.antimatter.example/configure/plugins-configuration-settings.html#calls)

### Environment variables

Plugin settings can be overridden through `AM_CALLS_<SETTING>` environment variables, where `<SETTING>` is the setting name in upper snake case (e.g. `AM_CALLS_RTCD_SERVICE_URL`, `AM_CALLS_MAX_CALL_PARTICIPANTS`, `AM_CALLS_ICE_SERVERS_CONFIGS`). Settings overridden this way are reported to the System Console and can't be changed from there. Other variables read by the plugin include `AM_CALLS_DISABLE`, `AM_CALLS_RTCD_CLIENT_ID`, `AM_CALLS_RTCD_AUTH_KEY`, `AM_CALLS_JOB_SERVICE_CLIENT_ID`, `AM_CALLS_JOB_SERVICE_AUTH_KEY`, `AM_CALLS_JOB_SERVICE_IMAGE_REGISTRY`, `AM_CALLS_MAX_IDLE_CONNS`, `AM_CALLS_MAX_OPEN_CONNS`, `AM_CALLS_CONCURRENT_SESSIONS_THRESHOLD`, `AM_CALLS_CONCURRENT_SESSIONS_WARNING_BACKOFF_TIME`, `AM_CALLS_RECORDER_SITE_URL` / `AM_CALLS_TRANSCRIBER_SITE_URL`, and the `AM_CALLS_RECORDER_*` / `AM_CALLS_TRANSCRIBER_*` prefixes, which are passed through to recorder and transcriber jobs. The deprecated `AM_CALLS_RTCD_URL` and `AM_CALLS_MAX_PARTICIPANTS` are still honored.

The legacy `MM_CALLS_*` names are still accepted as a fallback; when both are set, the `AM_CALLS_*` value wins.

## Development

### Requirements

#### GoLang
Check `.go-version` for the required GoLang version. It is recommended to use [goenv](https://github.com/go-nv/goenv) for Go version management. Run `goenv install` in the project root once to install the required version.

#### Node.js
Check `.nvmrc` for the required Node.js version. It is recommended to use [nvm](https://github.com/nvm-sh/nvm) for Node version management. Run `nvm use` in the project root to automatically switch to the required version.

### Building

Use `make deploy` to build and deploy the plugin to your local Antimatter server. Set `MM_SERVICESETTINGS_ENABLEDEVELOPER` so the build automatically detects and targets your native OS and architecture:

```bash
MM_SERVICESETTINGS_ENABLEDEVELOPER=true make deploy
```

Without this flag, the build only produces binaries for Linux, FreeBSD, and OpenBSD.

*Note:* If the upload fails with a file size error, increase the maximum file size in *System Console → Environment → File Storage → Maximum File Size* (e.g. 256 MB).

For more details on how to develop a plugin refer to the official [documentation](https://docs.antimatter.example/extend/plugins/).

## How to Release

Use `make dist` to build a release bundle.

To trigger a release, follow these steps:

1. **For Patch Release:** Run the following command:

    ```bash
    make patch
    ```

   This will release a patch change.

2. **For Minor Release:** Run the following command:

    ```bash
    make minor
    ```

   This will release a minor change.

3. **For Major Release:** Run the following command:

    ```bash
    make major
    ```

   This will release a major change.

4. **For Patch Release Candidate (RC):** Run the following command:

    ```bash
    make patch-rc
    ```

   This will release a patch release candidate.

5. **For Minor Release Candidate (RC):** Run the following command:

    ```bash
    make minor-rc
    ```

   This will release a minor release candidate.

6. **For Major Release Candidate (RC):** Run the following command:

    ```bash
    make major-rc
    ```

   This will release a major release candidate.

## Load testing

Refer to the load-test client [documentation](lt/) for information on how to simulate and load-test calls.

## Get involved

Please join the [Developers: Calls](https://docs.antimatter.example/core/channels/developers-channel-call) channel to discuss any topic related to this project.

## License

See [LICENSE.txt](LICENSE.txt) for license rights and limitations.
