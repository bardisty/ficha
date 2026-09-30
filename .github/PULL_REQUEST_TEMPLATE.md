<!-- Title: area: what changed, lower case after the colon, such as
     "watch: order agents by start". It's this PR's line in the release
     notes. CONTRIBUTING.md lists the areas. -->

## What and why



## For the release notes

Does this change output, flags, exit codes, or json keys or csv columns that scripts rely on? Describe it for the release notes, with the old and new jq path where there is one. If not, delete this section.



## Checklist

- [ ] `make ci` passes
- [ ] Changed how something renders? Before and after captures from the synthetic fixture are above, never output from your own transcripts. See [Seeing your change](https://github.com/bardisty/ficha/blob/main/CONTRIBUTING.md#seeing-your-change)
- [ ] `VERSION` bumped if the CLI changed. Docs and tooling don't bump; see [CONTRIBUTING.md](https://github.com/bardisty/ficha/blob/main/CONTRIBUTING.md)
