import { describe, expect, it } from 'vitest';
import { usagePercent, speedPercent } from '@/components/bandwagon/usage';

describe('provider usage presentation', () => {
  it('keeps unknown capacity distinct from idle and handles bytes vs bits', () => {
    expect(speedPercent(12_500_000, 1000)).toBe(10);
    expect(speedPercent(0, 1000)).toBe(0);
    expect(speedPercent(12_500_000, 0)).toBeNull();
    expect(usagePercent(0, 0)).toBeNull();
  });
  it('does not disguise over-quota usage or invalid measurements', () => {
    expect(usagePercent(120, 100)).toBe(120);
    expect(usagePercent(-1, 100)).toBeNull();
    expect(usagePercent(Number.NaN, 100)).toBeNull();
    expect(usagePercent(1, Number.POSITIVE_INFINITY)).toBeNull();
  });
});
