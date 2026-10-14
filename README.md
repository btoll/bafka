# bafka

`bafka` will take a [sitemap], parse it and then send `HEAD` and `GET` requests to all the links found in each page of the sitemap.

> See the fine article [On Link Scanning And systemd](https://benjamintoll.com/2026/10/15/on-link-scanning-and-systemd/) for more information.

## Usage

```go
Usage of ./bafka:
  -output string
        Instead of writing to stdout, write to a file.
  -sitemap string
        The list of available links. (default "sitemap.xml")
  -uri string
        The target site.
  -workers int
        The number of workers in the worker pool. (default 20)
```

If an `--output` file is given, the basename is taken from the given path and the file written to `/tmp/BASENAME`.   This is [a security feature of Go]; the tool is only allowing writes to `/tmp`.

In other words `--output /tmp/foo.jsonl` and `--output foo.jsonl` are the same.

## `systemd-timer`

If you'd like, there are two `systemd` unit files included in the repository that will allow you to register the tool as a cron job with `systemd`.

Install them:

```bash
$ mkdir -p ~/.config/systemd/user/
$ cp systemd/bafka.timer ~/.config/systemd/user/
$ cp systemd/bafka.service ~/.config/systemd/user/
$ systemctl --user daemon-reload
$ systemctl --user enable --now bafka.timer
```

Once registered with `systemd`, you can use `systemctl` to control the lifecycle of the units.  For example:

```bash
$ systemctl --user status bafka.timer
$ systemctl --user stop bafka.service
```

List all timers on the system:

```bash
$ systemctl --user list-timers bafka.timer
NEXT                         LEFT LAST                          PASSED UNIT        ACTIVATES
Thu 2026-10-15 02:00:00 EDT 43min Thu 2026-10-15 01:10:47 EDT 5min ago bafka.timer bafka.service

1 timers listed.
Pass --all to see loaded but inactive timers, too.
```

The `bafka.service` currently needs the `graphical-session.target` collection to be enacted because it relies upon the `notify-send` binary which needs a fully graphical environment in which to execute.  Change this `target` to your preference if this isn't desired.

## License

[GPLv3](COPYING)

## Author

[Benjamin Toll](https://benjamintoll.com)

[sitemap]: https://en.wikipedia.org/wiki/Site_map
[a security feature of Go]: https://go.dev/blog/osroot#introducing-osroot

