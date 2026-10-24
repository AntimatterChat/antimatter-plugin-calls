# rtcd for Antimatter

This is [rtcd](https://github.com/mattermost/rtcd) v1.2.6 with Antimatter's changes, used by the Calls app through a
`replace` in its go.mod (both as the app's embedded RTC server and when building an rtcd service). Upstream's tests
and test files are left out. The changes, each marked "Antimatter":

- Deafening a session (`service/rtc/deafen.go`, the `DeafenMessage` and `UndeafenMessage` messages, and the
  `deafened` session property): a deafened session receives no audio, for the moderators' server deafen.

To move to a newer rtcd, copy it over this folder (without its tests and test files) and apply the changes again.

# `rtcd`

`rtcd` (**r**eal-**t**ime **c**ommunication **d**aemon) is a service built to offload WebRTC and media processing tasks from [Mattermost Calls](https://github.com/mattermost/mattermost-plugin-calls) in order to efficiently support scalable and secure deployments of the plugin on both on-prem and cloud environments.

## Getting started

Refer to our [getting started guide](docs/getting_started.md) on how to quickly get calls working through `rtcd`.

## Running

`make go-run`

## Testing

`make test`

## Configuration

Configuration is documented in-place through the [`config.sample.toml`](config/config.sample.toml) file.

## Documentation

Documentation and implementation details can be found in the [`docs`](docs/) folder.

## Get involved

Please join the [Project: rtcd](https://community.mattermost.com/core/channels/project-rtcd) channel to discuss any topic related to this project.

## License

See [LICENSE.txt](LICENSE.txt) for license rights and limitations.

