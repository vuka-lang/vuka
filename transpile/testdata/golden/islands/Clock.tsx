import React from 'react'
interface Props { zone: string; when: Date; tick?: number; onTick?: (n: number) => void }
export default function Clock({ zone, tick = 1 }: Props) { return <time>{zone}</time> }
