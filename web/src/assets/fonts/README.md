# Local fonts

- `DMSans-Latin-Variable.woff2`: unmodified upright DM Sans Latin variable font (weights 100–900), supplied by the Slate handoff. License: `DM-Sans-LICENSE.txt`.
- `JetBrainsMono-Variable.ttf`: unmodified upright JetBrains Mono v2.304 (weights 100–800). License: `OFL.txt`.

Both fonts are bundled application assets, not remote requests or build-time imports from `refs/`. DM Sans is used for UI/prose; JetBrains Mono for source, paths and revisions. The supplied DM Sans subset is Latin only; fallback rendering does not establish support for other scripts. Ligatures and synthetic styles are disabled by the application foundation.
