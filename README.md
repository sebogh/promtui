# promtui

CLI for "tailing" a Prometheus metrics-endpoint (potentially useful for debugging purposes).

![screenshot](docs/screenshot.png)

## Install

```sh
go install https://github.com/sebogh/promtui@latest
```

## Usage

Run like: 

```sh
./promtui 
``` 

which will tail the metrics from the default endpoint: `http://localhost:9090/metrics`.

See `./promtui --help` for all available options.
