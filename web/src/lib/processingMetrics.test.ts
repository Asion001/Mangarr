import { describe, it, expect } from 'vitest';
import { sizeChange, sizeChangeLabel, throughput, signedBytes } from './processingMetrics';
describe('processing size and speed',()=>{
  it('distinguishes growth, reduction and unchanged output',()=>{
    expect(sizeChangeLabel(100,153)).toBe('+53%');
    expect(sizeChangeLabel(100,47)).toBe('-53%');
    expect(sizeChangeLabel(100,100)).toBe('0%');
    expect(sizeChangeLabel(100,0)).toBe('-100%');
    expect(sizeChange(0,100)).toBeNull();
    expect(sizeChangeLabel(-1,100)).toBe('—');
    expect(sizeChangeLabel(100,NaN)).toBe('—');
    expect(signedBytes(-1024)).toBe('−1 KB');
  });
  it('reports the 13-page, 17m26s example in pages per minute',()=>{
    expect(throughput(13,1046)).toBe('0.746 p/min');
    expect(throughput(60,10)).toBe('6 p/s');
    expect(throughput(0,10)).toBe('—');
    expect(throughput(13,0)).toBe('—');
    expect(throughput(1,1000000)).not.toBe('0 p/min');
  });
});
