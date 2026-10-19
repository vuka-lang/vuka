interface Props {
  id: number
  name: string
}

export function mount(el: HTMLElement, props: Props) {
  el.textContent = `${props.name} is user ${props.id}.`
  return {
    update(p: Props) {
      el.textContent = `${p.name} is user ${p.id}.`
    },
    unmount() {},
  }
}
