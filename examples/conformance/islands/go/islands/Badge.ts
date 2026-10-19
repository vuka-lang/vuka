import { island } from 'vuka:island'

interface Badge {
  label: string
  count: number
}

export default island<Badge>((el, props) => {
  el.textContent = `${props.label} (${props.count})`
})
