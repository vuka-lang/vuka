import { pageIsland } from 'vuka:island'
import type { Board } from 'vuka:live'

export default pageIsland<Board>((el, live) => {
  const draw = () => {
    el.textContent = `${live.q.value}: ${(live.rows.value || []).length} of ${live.total.value ?? '?'} (${live.seen.value} new)`
  }
  draw()
  return { update: draw }
})
