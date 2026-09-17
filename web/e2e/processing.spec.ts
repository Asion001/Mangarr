import { test, expect } from '@playwright/test';
test('processing growth has one positive sign and readable mobile cards',async({page})=>{
 await page.setViewportSize({width:390,height:844});
 await page.route('**/api/v1/**',async route=>{
  const p=new URL(route.request().url()).pathname;
  let body:unknown;
  if(p.endsWith('/auth/status'))body={authenticated:true,authDisabled:true,account:{kind:'anonymous',id:0,permissions:['admin']}};
  else if(p.endsWith('/processing'))body={engines:[],state:{},pending:0,failed:0,processed:1,spaceSaved:0,spaceAdded:5300000,netSpaceSaved:-5300000,pagesPerMinute:13/1046*60,etaSeconds:0,active:[],recent:[{seriesId:1,seriesTitle:"I'm Not a Soccer Genius!",chapter:'86',sizeOriginal:10000000,size:15300000,pages:13,seconds:1046,processedAt:new Date().toISOString()}]};
  else if(p.endsWith('/processing/history'))body=[];
  else {await route.fulfill({status:503,json:{detail:'Not part of this fixture'}});return;}
  await route.fulfill({json:body});
 });
 await page.goto('/system/status');
 const card=page.locator('article').filter({hasText:"I'm Not a Soccer Genius!"});
 await expect(card).toBeVisible();
 await expect(card).toContainText('+53%');
 await expect(card).toContainText('0.746 p/min');
 await expect(card).toContainText('17m 26s');
 await expect(card.getByText('Size',{exact:true})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
