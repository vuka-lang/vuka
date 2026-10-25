interface Props { label: string; count: number }

export function mount(el: HTMLElement, props: Props) {
  function update(next: Props) {
    el.textContent = `${next.label} (${next.count})`
  }
  update(props)
  return { update }
}
