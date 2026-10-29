# FIT test corpus

Anonymized copies of real activities, used by the FIT parser tests. Never commit an original.

| File | Activity |
| --- | --- |
| `half_marathon.fit` | Outdoor run, Forerunner 255 |
| `intervals.fit` | Outdoor interval run, Forerunner 255 |
| `zwift.fit` | Virtual ride written by Zwift |
| `bike.fit` | Outdoor ride, Forerunner 255 |
| `hike.fit` | Hike, Forerunner 255 |
| `pool_swim.fit` | Pool swim, Forerunner 255 |
| `strength.fit` | Strength workout, Forerunner 255 |

Make each file with:

```bash
go run ./cmd/anonymize original.fit testdata/fit/<name>.fit
```

`anonymize` shifts every longitude so the track starts on a neutral meridian in the Atlantic (latitudes are kept, so distances stay exact), renumbers device serial numbers and blanks text in fields the FIT profile does not describe (Garmin keeps the owner's name, sensor serials and the activity's city there). Everything else is kept: timestamps, heart rate, thresholds and the user profile's numbers (weight, height, gender, resting HR). Check you are happy publishing those before committing.

The Zwift file's first position has a longitude of about 0, so its virtual track lands at an arbitrary longitude. Virtual activities never get a map, so this does not matter.
