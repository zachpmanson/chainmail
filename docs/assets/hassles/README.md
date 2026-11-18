# Hassles favicon artwork

`dotgrid-26W12-897056.svg` is the owner's unchanged original, identified in the
November 16 testing-group message as the latest revision. Source:

https://chat.zachmanson.com/upload/09cb0ea18fd5b4c5eee4248e585e145042838456/KCRhPJ5IWzr52l1zccJjVzV5wFIUopp5iFiweciJ/dotgrid-26W12-897056.svg

SHA256: `62359e73eaaa3be5937e1acce6259161f77f47b69c5a8ba6bb54b34f54eea842`.
The Android Hassles app used the identical source at commit `0fd2e47`.

`fixtures/hassles-favicon.svg` changes only the root canvas dimensions to
320 × 320 and adds `viewBox="-40 5 320 320"`. Paths, strokes and colours are
unchanged. Visible bounds including strokes are x=67.5…172.5, y=37.5…292.5;
the square canvas centres the artwork with 32.5 units of vertical margin on
each side (about 80% height). Vite serves it at `/hassles-favicon.svg`.

The document uses this favicon only while `HasslesView` is mounted; its effect
cleanup restores the previous favicon attributes and title.
