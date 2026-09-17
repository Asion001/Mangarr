import { test, expect } from '@playwright/test';
test('switches languages and keeps an anonymous preference after reload',async({page})=>{
  await page.route('**/api/v1/**',async route=>{
    const p=new URL(route.request().url()).pathname;
    const body=p.endsWith('/auth/status')?{authenticated:true,authDisabled:true,account:{kind:'anonymous',id:0,permissions:[]}}:{};
    await route.fulfill({json:body});
  });
  await page.goto('/account');
  await page.getByRole('combobox').first().selectOption('ru');
  await expect(page.locator('html')).toHaveAttribute('lang','ru');
  await expect(page.getByRole('heading',{name:'Мой аккаунт'})).toBeVisible();
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('lang','ru');
  await page.getByRole('combobox').first().selectOption('uk');
  await expect(page.locator('html')).toHaveAttribute('lang','uk');
  await expect(page.getByRole('heading',{name:'Мій обліковий запис'})).toBeVisible();
});
