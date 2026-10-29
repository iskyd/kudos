# GPX test files

| File | Content |
| --- | --- |
| `run.gpx`, `ride_no_hr_no_ele.gpx` | Small synthetic files for the parser's unit and golden tests |
| `half_marathon.gpx.gz`, `intervals.gpx.gz`, `bike.gpx.gz`, `zwift.gpx.gz`, `pool_swim.gpx.gz` | Garmin Connect's GPX export of the activity with the same name in `testdata/fit/`, anonymized |

The exports are checked against the FIT files by `internal/importer/crossformat_test.go`. Make each with:

```bash
go run ./cmd/anonymize original.gpx testdata/gpx/<name>.gpx
gzip -9n testdata/gpx/<name>.gpx  # keeps files under the 1 MB pre-commit limit
```

`anonymize` shifts longitudes exactly like it does for the FIT file, so positions match it, blanks track names (often the city) and replaces the device serial. Never commit an original.
