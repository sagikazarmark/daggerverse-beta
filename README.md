# Daggerverse Beta

Beta Dagger modules.

```bash
dagger check -l -q 2>/dev/null | tail -n +2 | cut -d' ' -f1 | jq -R -s 'split("\n") | map(select(. != ""))' | yq '.jobs.check.strategy.matrix.check = load("/dev/stdin")' .github/workflows/dagger.yaml.tpl > .github/workflows/dagger.yaml
```
