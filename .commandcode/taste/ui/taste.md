# UI

- Expects third-party artwork to be picked for the app's dark interface rather than just taken as-is: when a package ships light/dark variants of an asset (CEP `<Icons>` `Normal` vs `DarkNormal`, UXP `icons` entries tagged with `theme`), choose the variant drawn for a dark UI so nothing ends up dark-on-dark and unreadable. Confidence: 0.7
- Dislikes scrollbars that appear with nothing to scroll; a pane should size/fill so no unnecessary scroll container is shown (rather than tuning hardcoded height constants to hide it). Confidence: 0.6
- Wants UI consistency ("统一"): sibling option buttons must be the same height/size regardless of how long their labels are (pin a fixed height and shorten the copy so neither wraps, rather than letting one grow taller), and a single action keeps one name across modes instead of the label varying with the mode. Confidence: 0.6
- Prefers compact, dense panel layouts: trims card padding, gaps, metadata-cell padding, chip heights and button heights rather than leaving generous spacing. Confidence: 0.6
- When an item would overwrite something already installed, shows the version transition as `old → new` with the old value muted/struck-through and the new value highlighted in the accent color. Confidence: 0.6
- Wants sideloading (writing directly into the user directory, no Adobe installer service) to be the default install method. Confidence: 0.55
