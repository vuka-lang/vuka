```vuka-html
<design.Tabs id="plan" default="monthly">
  <slot:tab value="monthly" label="Monthly"><p>Billed every month.</p></slot:tab>
  <slot:tab value="yearly" label="Yearly"><p>Billed once a year.</p></slot:tab>
  <slot:tab value="lifetime" label="Lifetime" disabled><p>Not offered.</p></slot:tab>
</design.Tabs>
```

A [stateful component](/ui/stateful): the tab shown is its own state on the
server, so a click switches it over the live connection and the rest of the
page keeps its state. Rendered on a page that isn't live, it shows its
first tab. The markup is `role="tablist"`, `role="tab"` buttons with
`aria-selected` and `aria-controls`, and the `role="tabpanel"` labelled by
its tab.
