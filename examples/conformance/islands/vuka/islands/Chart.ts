import { island } from 'vuka:island'

interface Chart {
  title: string
  points: number[]
}

export default island<Chart>((el, props, ctx) => {
  const draw = (p: Chart) => {
    const title = document.createElement('b')
    title.className = 'title'
    title.textContent = p.title
    const points = document.createElement('span')
    points.className = 'points'
    points.textContent = p.points.join(',')
    el.replaceChildren(title, ' ', points)
  }
  draw(props)
  el.addEventListener('dblclick', () => ctx.emit('select', { from: 0, to: props.points.length - 1 }))
  return { update: draw }
})
