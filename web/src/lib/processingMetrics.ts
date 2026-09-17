import { getLocale, t } from './i18n/core';
import { bytes } from './format';

/** A positive change is growth; an unknown original must not imply savings. */
export function sizeChange(before:number, after:number):number|null {
  return Number.isFinite(before) && Number.isFinite(after) && before>0 && after>=0 ? (after-before)/before : null;
}
export function sizeChangeLabel(before:number, after:number):string {
  const delta=sizeChange(before,after);
  return delta===null ? '—' : new Intl.NumberFormat(getLocale(),{style:'percent',maximumFractionDigits:0,signDisplay:'exceptZero'}).format(delta);
}
export function throughput(pages:number, seconds:number):string {
  if(!Number.isFinite(pages)||!Number.isFinite(seconds)||pages<=0||seconds<=0)return '—';
  const rate=pages/seconds;
  const value=rate>=1?rate:rate*60;
  // Three significant digits keeps very slow work visible instead of rounding to zero.
  const number=new Intl.NumberFormat(getLocale(),{maximumSignificantDigits:3}).format(value);
  return `${number} ${rate>=1?t('p/s'):t('p/min')}`;
}
export function signedBytes(value:number):string {
  return `${value>0?'+':value<0?'−':''}${bytes(Math.abs(value))}`;
}
