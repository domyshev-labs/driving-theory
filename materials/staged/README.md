# Reviewed contributions

`questions.json` archives reviewed questions independently of the active question bank. Every record uses the application's stable opaque question ID and image naming convention. Captured wording, answer choices, English explanations, explanation HTML and illustrations are retained. Missing Spanish or Russian explanations remain empty rather than being invented. Topic assignments were reviewed from prompts and choices only.

`capture_completed: true` marks a completed capture. `training_enabled: false` keeps the contribution out of the active bank and topic libraries. All contributions start disabled. A record whose ID already exists in the active bank is an additional capture of that question, not another training question. Archiving it does not disable the existing question or change its progress.

To activate selected contributions, set their `training_enabled` to `true`, then run:

```bash
python3 scripts/rebuild-library.py
python3 scripts/validate-content.py
python3 scripts/audit-topics.py
```

The builder adds only new IDs and preserves their reviewed topic assignments. Existing questions and classifications remain unchanged. A matching existing record is not duplicated; a conflicting existing record or `review_required: true` blocks activation before files are written. Resolve any flagged answer discrepancy explicitly before enabling it. Never recompute IDs after wording, image or topic edits.

Activation imports content into the regular bank. Setting the archival flag back to false does not remove an already imported question or erase saved answers. Removing published content requires a separate reviewed change.

Primary illustrations use `/questions/<id>_1.jpg`. Explanation illustrations use subsequent numbers and are also retained in `help_html` and `help_images`. The current interface continues displaying the first image and plain-text explanations; this archive does not introduce HTML rendering.
