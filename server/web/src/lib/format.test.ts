import { describe, expect, it } from 'vitest'
import { countryCentroid, countryName, flag, fmtDelta, fmtNum, fmtRatio } from './format'

describe('format', () => {
  it('compacts numbers', () => {
    expect(fmtNum(980)).toBe('980')
    expect(fmtNum(12400)).toBe('12.4k')
    expect(fmtNum(10000)).toBe('10k')
    expect(fmtNum(1500000)).toBe('1.5m')
  })
  it('formats deltas', () => {
    expect(fmtDelta(108, 100)).toBe('+8%')
    expect(fmtDelta(97, 100)).toBe('-3%')
    expect(fmtDelta(5, 0)).toBe('new')
    expect(fmtDelta(0, 0)).toBe('')
  })
  it('formats ratios', () => {
    expect(fmtRatio(162, 100)).toBe('1.62')
    expect(fmtRatio(200, 100)).toBe('2')
    expect(fmtRatio(0, 0)).toBe('0')
  })
  it('knows countries', () => {
    expect(countryName('GB')).toBe('United Kingdom')
    expect(countryName('')).toBe('Unknown')
    expect(countryCentroid('JP')?.length).toBe(2)
    expect(countryCentroid('XX')).toBeNull()
    expect(flag('GB')).toBe('🇬🇧')
    expect(flag('')).toBe('')
  })
})
