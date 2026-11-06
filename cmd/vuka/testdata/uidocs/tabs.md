```vuka-html
<script lang="go">
package settings

import "github.com/vuka-lang/ui/v2"

// Plan is the billing settings.
type Plan struct {
	plan ui.State[string]
}
</script>

<section>
  <design.Tabs id="plan" value={plan} default="monthly">
    <slot:tab value="monthly" label="Monthly"><p>Billed every month.</p></slot:tab>
    <slot:tab value="yearly" label="Yearly"><p>Billed once a year.</p></slot:tab>
    <slot:tab value="lifetime" label="Lifetime" disabled><p>Not offered.</p></slot:tab>
  </design.Tabs>
  <design.Button variant={design.ButtonLink} onClick={plan = "yearly"}>Save two months</design.Button>
</section>
```

The tab shown is [browser state](/ui/state): a `ui.State[string]` holding
the shown tab's value, passed as `value`. A click on a tab writes it, so the
panel switches at once with no round trip, and anything else writing it —
a button, a live handler's `p.plan = "yearly"` — switches it too. Without
`value` the tabs keep a state of their own. Until a tab is picked, the
`default` one shows, and the server renders it so.

Every panel is rendered, the others `hidden`. A live page that wants to hear
the switch declares the state with
[`@ui.Notify`](/ui/state#notify): `plan ui.State[string] @ui.Notify(OnPlan)`.
The markup is `role="tablist"`, `role="tab"` buttons with `aria-selected`
and `aria-controls`, and the `role="tabpanel"` labelled by its tab.
