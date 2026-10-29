```vuka-html
<design.Card title="Team" description="Who can see this project.">
  <slot:action><design.Button size={design.SizeSm}>Invite</design.Button></slot:action>
  <p>Three members.</p>
  <slot:footer>Updated today</slot:footer>
</design.Card>
```

The header (title, description and the `action` slot beside them), the
content and the `footer` slot: each part is left out when empty.
