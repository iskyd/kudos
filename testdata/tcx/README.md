# TCX test files

| File | Content |
| --- | --- |
| `run.tcx`, `ride_no_hr_no_ele.tcx` | Small synthetic files for the parser's unit and golden tests |
| `half_marathon.tcx.gz`, `intervals.tcx.gz`, `bike.tcx.gz`, `zwift.tcx.gz`, `pool_swim.tcx.gz` | Garmin Connect's TCX export of the activity with the same name in `testdata/fit/`, anonymized |

The exports are checked against the FIT files by `internal/importer/crossformat_test.go`. Make each with:

```bash
go run ./cmd/anonymize original.tcx testdata/tcx/<name>.tcx
gzip -9n testdata/tcx/<name>.tcx  # keeps files under the 1 MB pre-commit limit
```

`anonymize` shifts longitudes exactly like it does for the FIT file, so positions match it, blanks track names (often the city) and replaces the device serial. Never commit an original.
