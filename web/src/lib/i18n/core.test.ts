import { describe, expect, it, afterEach } from 'vitest';
import { resolveLocale, translate, plural, setLocale } from './core';
import { date, relative } from '../format';
import catalog from './messages.json';
afterEach(()=>setLocale('en'));
describe('interface locales',()=>{
  it('normalizes Ukrainian aliases and browser preferences',()=>{
    expect(resolveLocale('ua-UA')).toBe('uk');
    expect(resolveLocale('auto',['de-DE','uk-UA','en'])).toBe('uk');
    expect(resolveLocale('ru-RU')).toBe('ru');
    expect(resolveLocale('unknown')).toBe('en');
  });
  it('keeps every message and interpolation in both translations',()=>{
    const tokens=(s:string)=>(s.match(/\{\w+\}/g)??[]).sort();
    for(const [key,row] of Object.entries(catalog)) for(const locale of ['ru','uk'] as const){
      expect(row[locale].trim(),key).not.toBe('');
      expect(tokens(row[locale]),key).toEqual(tokens(key));
    }
  });
  it('falls back without modifying source-owned text',()=>{
    expect(translate('A title supplied by a source','uk')).toBe('A title supplied by a source');
    expect(translate('Read','uk')).toBe('Читати');
  });
  it('supports Slavic plural forms',()=>{
    const forms={one:'{count} глава',few:'{count} главы',many:'{count} глав',other:'{count} главы'};
    expect(plural(1,forms,'ru')).toBe('1 глава');
    expect(plural(2,forms,'ru')).toBe('2 главы');
    expect(plural(11,forms,'ru')).toBe('11 глав');
    expect(plural(21,forms,'ru')).toBe('21 глава');
  });
  it('uses the interface locale for date and relative time',()=>{
    setLocale('uk');
    expect(date('2026-09-17T12:00:00Z')).toBe(new Date('2026-09-17T12:00:00Z').toLocaleDateString('uk'));
    expect(relative(new Date(Date.now()-120000).toISOString())).toBe('2 хвилини тому');
  });
});
