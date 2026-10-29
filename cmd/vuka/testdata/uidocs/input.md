```vuka-html
<design.Label for="email">Email</design.Label>
<design.Input id="email" name="email" type="email" required invalid={err != ""} />
```

`invalid` sets `aria-invalid` and draws the field in the destructive
colour.
