import { createContext, useContext, useEffect, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, unwrap } from '../api/client';
import { useAccount } from './account';
import { getLocale, resolveLocale, setLocale, subscribeLocale, type LocalePreference } from './i18n/core';

export type UIPreferences = {locale:LocalePreference; mode:'reading'|'editing'};
const defaults: UIPreferences = {locale:'auto',mode:'reading'};
const Ctx = createContext<{preferences:UIPreferences; save:(p:Partial<UIPreferences>)=>Promise<void>; canEdit:boolean; saving:boolean; error:unknown}>({preferences:defaults,save:async()=>{},canEdit:false,saving:false,error:null});
function readLocal(key:string): UIPreferences {
  try { const v=JSON.parse(localStorage.getItem(key) || '{}'); return {locale:['auto','en','ru','uk'].includes(v.locale)?v.locale:'auto',mode:v.mode==='editing'?'editing':'reading'}; } catch {return defaults;}
}
export function UIPreferencesProvider({children}:{children:ReactNode}) {
  const {account,can} = useAccount();
  const personal = account?.kind === 'user';
  const canEdit = can(['library.manage','requests.manage']);
  const key = `mangarr:ui:${account?.kind ?? 'guest'}:${account?.id ?? 0}`;
  const [local,setLocal] = useState(() => readLocal(key));
  const qc = useQueryClient();
  useEffect(()=>{setLocal(readLocal(key));},[key]);
  const query = useQuery({queryKey:['ui-preferences',account?.id],enabled:personal,queryFn:()=>unwrap(api.GET('/api/v1/me/ui-preferences'))});
  const preferences:UIPreferences = {...(personal ? query.data ?? defaults : local),mode:canEdit ? (personal ? query.data?.mode ?? 'reading' : local.mode) : 'reading'};
  useEffect(()=>{setLocale(resolveLocale(preferences.locale,navigator.languages));},[preferences.locale]);
  const mutation = useMutation({mutationFn:async(p:Partial<UIPreferences>)=>{
    // The API response also contains updatedAt. Build the strict request shape
    // explicitly so response-only fields are never sent back to the server.
    const next:UIPreferences={locale:p.locale ?? preferences.locale,mode:p.mode ?? preferences.mode};
    if(!canEdit)next.mode='reading';
    if(personal){const result=await unwrap(api.PUT('/api/v1/me/ui-preferences',{body:next}));qc.setQueryData(['ui-preferences',account?.id],result);}
    else {localStorage.setItem(key,JSON.stringify(next));setLocal(next);}
  }});
  return <Ctx.Provider value={{preferences,save:async(p)=>{await mutation.mutateAsync(p)},canEdit,saving:mutation.isPending,error:query.error ?? mutation.error}}>{children}</Ctx.Provider>;
}
export const useUIPreferences = () => useContext(Ctx);
export function useUIMode(){const v=useUIPreferences();return {...v,editing:v.canEdit&&v.preferences.mode==='editing'};}
export const useLocale = () => useSyncExternalStore(subscribeLocale,getLocale,getLocale);
