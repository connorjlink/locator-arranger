# LOCATOR-ARRANGER
A locator-to-HTML smart shelf-packing photo album arranger with customizable styling. Designed to work with Locator.
> [locator](https://github.com/connorjlink/locator).

## EXAMPLE USAGE
```bash
go run ./arranger.go
  -summaries ./summaries
  -photos ./photos
  -maps ./maps
  -theme ./themes/default_dark.json
  -pack chronological
  -out ./album.html
```

## NOTES
See locator/themes/default_dark.json for an example themefile.
