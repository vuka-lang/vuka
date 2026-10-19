import { island } from 'vuka:island'

interface Chart {
  title: string
  points: number[]
}

export default island<Chart>((el, props, ctx) => {
  const draw = (p: Chart) => {
    el.innerHTML = `<b class="title">${p.title}</b> <span class="points">${p.points.join(',')}</span>`
  }
  draw(props)
  el.addEventListener('dblclick', () => ctx.emit('select', { from: 0, to: props.points.length - 1 }))
  return { update: draw }
})
