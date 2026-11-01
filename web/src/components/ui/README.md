# Slate controls

- `UiButton`: primary, secondary, quiet and soft actions; `aria-pressed` supports toggles. `disabled` and `busy` retain keyboard focus for explanations, set `aria-disabled`, prevent activation and use `type="button"` to avoid submitting/resetting forms. Busy actions also set `aria-busy`; callers supply honest pending text and a description, not invented progress.
- `UiField`: labeled text/search input, textarea or native select (`type="select"` with value/label options). `hint` and textual `error` are associated alongside caller `aria-describedby` IDs. Errors set `aria-invalid`; unavailable/busy fields are natively disabled with readable hints. Do not infer provider availability from sample options.
- `UiNavigation`: ordinary destination links with `aria-current="page"`; no tab roles or arrow-key interception. Consumers can handle its `navigate` event and prevent default for local navigation; otherwise links follow their href normally. Use trusted application-owned destinations.
- `UiTabs`: actual in-page panels only; caller supplies matching panel IDs/labels. Roving Left/Right/Home/End focus remains distinct from destination navigation.
- `UiBadge`: noninteractive status text with semantic tones. Always supply words/icons, never color alone. It is not an automatic live region.
- `ScrollRegion`: labeled keyboard-focusable native scroll region, with visible focus and scrollbars. It does not move focus when content changes.

Major buttons/navigation links have minimum 44px targets; native selects use 48px. In-page content tabs use the 45px Slate navigation height. The reference's 30px diff toggles and 32px citations are desktop exceptions, not alternate sizes for primary controls or a touch-accessibility claim. Those excerpt-specific controls are not introduced here.

In fixture mode, `/?fixture=controls` exposes local state/keyboard specimens without runtime/API requests. Normal development and release ignore this selector. No control implements PR loading, model discovery, agent execution or durable saving.
