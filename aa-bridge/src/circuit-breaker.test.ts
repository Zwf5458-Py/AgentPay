import { describe, it, expect, vi } from 'vitest';
import { CircuitBreaker } from './circuit-breaker.js';

describe('CircuitBreaker', () => {
  it('should execute successfully when circuit is closed', async () => {
    const breaker = new CircuitBreaker({ threshold: 3, timeout: 1000, resetTimeout: 1000 });
    const fn = vi.fn().mockResolvedValue('success');

    const result = await breaker.execute(fn);
    expect(result).toBe('success');
    expect(fn).toHaveBeenCalledOnce();
    expect(breaker.getState()).toBe('closed');
  });

  it('should open circuit after threshold failures', async () => {
    const breaker = new CircuitBreaker({ threshold: 3, timeout: 1000, resetTimeout: 1000 });
    const fn = vi.fn().mockRejectedValue(new Error('fail'));

    for (let i = 0; i < 3; i++) {
      try {
        await breaker.execute(fn);
      } catch (e) {
        // expected
      }
    }

    expect(breaker.getState()).toBe('open');
    expect(breaker.getFailures()).toBe(3);
  });

  it('should reject immediately when circuit is open', async () => {
    const breaker = new CircuitBreaker({ threshold: 2, timeout: 5000, resetTimeout: 1000 });
    const fn = vi.fn().mockRejectedValue(new Error('fail'));

    // Trigger open
    for (let i = 0; i < 2; i++) {
      try {
        await breaker.execute(fn);
      } catch (e) {
        // expected
      }
    }

    expect(breaker.getState()).toBe('open');

    // Reset mock to verify it's not called when open
    fn.mockClear();

    // Next call should fail immediately without calling fn
    await expect(breaker.execute(fn)).rejects.toThrow('Circuit breaker is OPEN');
    expect(fn).not.toHaveBeenCalled();
  });

  it('should transition to half-open after timeout', async () => {
    const breaker = new CircuitBreaker({ threshold: 2, timeout: 100, resetTimeout: 1000 });
    const fn = vi.fn().mockRejectedValue(new Error('fail'));

    // Trigger open
    for (let i = 0; i < 2; i++) {
      try {
        await breaker.execute(fn);
      } catch (e) {
        // expected
      }
    }

    expect(breaker.getState()).toBe('open');

    // Wait for timeout
    await new Promise((resolve) => setTimeout(resolve, 150));

    // Should now be half-open and allow one call
    const successFn = vi.fn().mockResolvedValue('recovered');
    const result = await breaker.execute(successFn);
    expect(result).toBe('recovered');
    expect(breaker.getState()).toBe('closed');
  });

  it('should stay open if half-open probe fails', async () => {
    const breaker = new CircuitBreaker({ threshold: 1, timeout: 100, resetTimeout: 1000 });

    // Trigger open with 1 failure
    try {
      await breaker.execute(() => Promise.reject(new Error('fail')));
    } catch (e) {
      // expected
    }

    expect(breaker.getState()).toBe('open');

    // Wait for timeout
    await new Promise((resolve) => setTimeout(resolve, 150));

    // Half-open probe fails
    try {
      await breaker.execute(() => Promise.reject(new Error('fail again')));
    } catch (e) {
      // expected
    }

    expect(breaker.getState()).toBe('open');
    expect(breaker.getFailures()).toBe(2);
  });

  it('should timeout long-running calls', async () => {
    const breaker = new CircuitBreaker({ threshold: 5, timeout: 1000, resetTimeout: 100 });
    const slowFn = () => new Promise((resolve) => setTimeout(() => resolve('late'), 500));

    await expect(breaker.execute(slowFn)).rejects.toThrow('Circuit breaker timeout');
  });

  it('should reset circuit breaker state', async () => {
    const breaker = new CircuitBreaker({ threshold: 1, timeout: 1000, resetTimeout: 1000 });
    try {
      await breaker.execute(() => Promise.reject(new Error('fail')));
    } catch (e) {
      // expected
    }

    expect(breaker.getState()).toBe('open');
    breaker.reset();
    expect(breaker.getState()).toBe('closed');
    expect(breaker.getFailures()).toBe(0);
  });
});
