# Design-system presets

shadcn-svelte design-system presets we're evaluating. To switch, run the
command below from the `frontend/` directory, then review `git diff` — a preset
rewrites `components.json`, the theme variables in `src/routes/layout.css`, and
re-fetches the installed UI components (`button`, `card`, `avatar`,
`dropdown-menu`) in the new style.

| Preset code  | Status   | Notes                                          |
| ------------ | -------- | ---------------------------------------------- |
| `b5Jz95vQJ`  | Applied  | `lyra` style — sharp corners, compact, mono accents |
| `b4hZXSieQ`  | Backup   | Alternative to compare against                 |

## Switch to a preset

```sh
cd frontend
npx shadcn-svelte@latest init --preset <code> --overwrite \
  --css src/routes/layout.css \
  --lib-alias '$lib' \
  --components-alias '$lib/components' \
  --ui-alias '$lib/components/ui' \
  --utils-alias '$lib/utils' \
  --hooks-alias '$lib/hooks'
```

`--overwrite` and the explicit `--*-alias` flags keep the run non-interactive so
it doesn't re-prompt for paths. Our custom components (`top-bar.svelte`,
`user.svelte.ts`, the route pages) are not registry items, so they're left
untouched.
