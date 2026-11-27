// The Manage page's hangar (hangarlist.js): the Repair and Reconfigure dialogs, and the Rearm and Sell confirmations (confirm.js), each charging or paying
// the warchest.
const { test, expect, newAccount, startCampaign, setHangar, setWarchest, unit, STARTING } = require('./helpers');

// Fire Moth: crippled, with spent ammunition. Mist Lynx: truly destroyed. Kit Fox: structure damage. Hellbringer: a pristine omnimech.
const HANGAR = [
  unit('ua', 'BMDasher_Prime', { EngineHits: 2, AmmoBins: 2, AmmoUsed: 8 }),
  unit('ub', 'BMKoshi_E', { Destroyed: true, Locations: { CT: { Internal: 8 } } }),
  unit('uc', 'BMGrizzly'),
  unit('ud', 'BMUller_J', { Locations: { LA: { Armor: 3, Internal: 1 } } }),
  unit('ue', 'BMLoki_Prime')
];

async function managePage(page, warchest) {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina'], STARTING);
  await setHangar(page, HANGAR, 'refusalwar');
  await setWarchest(page, 'refusalwar', warchest);
  await page.goto('/refusalwar/manage/');
  const list = page.locator('#hangar-units-list');
  // The page shows the hangar set above, which replaced the one the campaign started with.
  await expect(list.locator('.mech-row')).toHaveCount(5);
  await expect(list.locator('.mech-row', { hasText: 'Fire Moth' }).locator('.mr-chip', { hasText: 'Crippled' })).toBeVisible();
  return list;
}

function row(list, name) { return list.locator('.mech-row', { hasText: name }); }
function warchest(page) { return page.locator('#hangar-units').getAttribute('data-warchest'); }

test('repairs a damaged mech, and refuses a repair the warchest cannot pay', async ({ page }) => {
  const list = await managePage(page, 4500);
  await expect(row(list, 'Grizzly').getByRole('button', { name: 'Repair' })).toBeDisabled();
  await row(list, 'Kit Fox').getByRole('button', { name: 'Repair' }).click();
  const dlg = page.locator('#repair-dialog');
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#repair-title')).toHaveText('Repair Kit Fox');
  const cost = dlg.locator('#repair-cost');
  await expect(cost).toHaveText(/^Cost \d+ SP\.\nWarchest 4,500 → [\d,]+ SP\.$/);
  // Each repair the unit needs is offered with its price; the dearest is chosen to begin with.
  const options = await dlg.locator('#repair-select option').allTextContents();
  expect(options.length).toBeGreaterThan(1);
  await dlg.locator('#repair-select').selectOption({ index: 0 });
  const price = Number(/Cost (\d+) SP/.exec(await cost.textContent())[1]);
  expect(price).toBeGreaterThan(0);
  await dlg.locator('#repair-apply').click();
  await expect(dlg).toBeHidden();
  await expect.poll(() => warchest(page)).toBe(String(4500 - price));

  // With almost nothing in the warchest a repair can't be paid for.
  await setWarchest(page, 'refusalwar', 1);
  await page.reload();
  await row(list, 'Fire Moth').getByRole('button', { name: 'Repair' }).click();
  await expect(dlg.locator('#repair-cost')).toHaveText(/^Cost [\d,]+ SP\.\nWarchest 1 SP\. Not enough funds\.$/);
  await expect(dlg.locator('#repair-cost')).toHaveClass(/short/);
  await expect(dlg.locator('#repair-apply')).toBeDisabled();
  await dlg.locator('#repair-cancel').click();
  await expect(dlg).toBeHidden();

  // Rearming it can't be paid for either: a notice instead of the confirmation.
  await row(list, 'Fire Moth').getByRole('button', { name: 'Rearm' }).click();
  const notice = page.getByRole('dialog', { name: 'Not enough funds' });
  await expect(notice).toContainText('Cost 20 SP. Warchest 1 SP. Not enough funds.');
  await notice.getByRole('button', { name: 'Close' }).click();

  // Nor a purchase.
  await page.locator('#purchase-open').click();
  const shop = page.locator('#purchase-dialog');
  await shop.locator('#purchase-search').fill('Grizzly');
  await shop.locator('.purchase-found .mech-row', { hasText: 'Grizzly' }).first().click();
  await expect(shop.locator('#purchase-cost')).toHaveText(/^Cost [\d,]+ SP\.\nWarchest 1 SP\. Not enough funds\.$/);
  await expect(shop.locator('#purchase-cost')).toHaveClass(/short/);
  await expect(shop.locator('#purchase-buy')).toBeDisabled();
});

test('rearms a mech after a confirmation', async ({ page }) => {
  const list = await managePage(page, 4500);
  await row(list, 'Fire Moth').getByRole('button', { name: 'Rearm' }).click();
  const confirm = page.getByRole('dialog', { name: 'Rearm Fire Moth?' });
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText('Cost 20 SP. Warchest 4,500 → 4,480 SP.');
  await confirm.getByRole('button', { name: 'Rearm for 20 SP' }).click();
  await expect(confirm).toBeHidden();
  await expect.poll(() => warchest(page)).toBe('4480');
});

test('sells a truly destroyed mech for a quarter of its BV', async ({ page }) => {
  const list = await managePage(page, 4500);
  await row(list, 'Mist Lynx').getByRole('button', { name: 'Sell' }).click();
  const confirm = page.getByRole('dialog', { name: 'Sell Mist Lynx?' });
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText('Truly destroyed: halved again, rounded down');
  const label = await confirm.getByRole('button', { name: /^Sell for \d+ SP$/ }).textContent();
  const value = Number(/(\d+)/.exec(label)[1]);
  await expect(confirm).toContainText('Warchest 4,500 → ' + (4500 + value).toLocaleString('en-US') + ' SP.');
  await confirm.getByRole('button', { name: /^Sell for/ }).click();
  await expect(confirm).toBeHidden();
  await expect(list.locator('.mech-row')).toHaveCount(4);
  await expect(row(list, 'Mist Lynx')).toHaveCount(0);
  await expect.poll(() => warchest(page)).toBe(String(4500 + value));
});

test('reconfigures an omnimech to another variant', async ({ page }) => {
  const list = await managePage(page, 4500);
  // Only a fully repaired and rearmed omnimech can be reconfigured.
  await expect(row(list, 'Hellbringer').getByRole('button', { name: 'Reconfigure' })).toBeEnabled();
  await row(list, 'Hellbringer').getByRole('button', { name: 'Reconfigure' }).click();
  const dlg = page.locator('#swap-dialog');
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#swap-title')).toHaveText('Reconfigure Hellbringer');
  await expect(dlg.locator('#swap-cost')).toHaveText('Choose another variant to reconfigure to.');
  await expect(dlg.locator('#swap-apply')).toBeDisabled();
  const values = await dlg.locator('#swap-select option:not([disabled])').evaluateAll((os) => os.map((o) => o.value).filter(Boolean));
  expect(values.length).toBeGreaterThan(0);
  await dlg.locator('#swap-select').selectOption(values[0]);
  await expect(dlg.locator('#swap-cost')).toHaveText(/^Cost \d+ SP\.\nWarchest 4,500 → [\d,]+ SP\.$/);
  await expect(dlg.locator('#swap-breakdown')).toContainText('50% of tonnage');
  const price = Number(/Cost (\d+) SP/.exec(await dlg.locator('#swap-cost').textContent())[1]);
  await dlg.locator('#swap-apply').click();
  await expect(dlg).toBeHidden();
  await expect.poll(() => warchest(page)).toBe(String(4500 - price));
});

test('shows unit cards on hover, and the condition pills their own', async ({ page }) => {
  // A card goes away when the page scrolls, as hovering a row out of view would make it: a tall window keeps every row in view.
  await page.setViewportSize({ width: 1280, height: 2400 });
  const list = await managePage(page, 4500);
  const tip = page.locator('#pill-tip');
  const moth = row(list, 'Fire Moth');
  await moth.locator('.mr-name').hover();
  await expect(tip).toBeVisible();
  await expect(tip.locator('.tip-head')).toContainText('Fire Moth');
  await expect(moth).toHaveClass(/tip-open/);
  // The pill's own card replaces it, and coming back off the pill brings the unit's card back.
  await moth.locator('.mr-chip', { hasText: 'Crippled' }).hover();
  await expect(tip.locator('.pill-tip-title')).toHaveText('Crippled');
  await moth.locator('.mr-name').hover();
  await expect(tip.locator('.tip-head')).toContainText('Fire Moth');
  await page.mouse.move(0, 0);
  await expect(tip).toBeHidden();

  // The Reconfigure dialog's rows have the same cards, shown inside the dialog.
  await row(list, 'Hellbringer').getByRole('button', { name: 'Reconfigure' }).click();
  const dlg = page.locator('#swap-dialog');
  await dlg.locator('#swap-sub .mech-row').first().hover();
  await expect(dlg.locator('#pill-tip')).toBeVisible();
  await expect(dlg.locator('#pill-tip .tip-head')).toContainText('Hellbringer');
});

test('sets the default number of players', async ({ page }) => {
  await managePage(page, 4500);
  const pick = page.locator('#overview-players-pick');
  await expect(pick.getByRole('button', { name: '1 player', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await pick.getByRole('button', { name: '2 players' }).click();
  await expect(pick.getByRole('button', { name: '2 players' })).toHaveAttribute('aria-pressed', 'true');
  await page.reload();
  await expect(pick.getByRole('button', { name: '2 players' })).toHaveAttribute('aria-pressed', 'true');
  await expect(pick.getByRole('button', { name: '1 player', exact: true })).toHaveAttribute('aria-pressed', 'false');
});

test('buys units from the Purchase dialog', async ({ page }) => {
  const list = await managePage(page, 4500);
  await page.locator('#purchase-open').click();
  const dlg = page.locator('#purchase-dialog');
  await expect(dlg).toBeVisible();
  // An empty cart costs nothing, and can't be bought.
  await expect(dlg.locator('#purchase-cost')).toHaveText('Cost 0 SP. Warchest 4,500 SP.');
  await expect(dlg.locator('#purchase-buy')).toBeDisabled();
  await dlg.locator('#purchase-search').fill('Grizzly');
  const found = dlg.locator('.purchase-found .mech-row', { hasText: 'Grizzly' }).first();
  await found.click();
  await expect(found).toContainText('In cart');
  await expect(dlg.locator('#purchase-staged .mech-row')).toHaveCount(1);
  const cost = dlg.locator('#purchase-cost');
  await expect(cost).toHaveText(/^Cost [\d,]+ SP\.\nWarchest 4,500 → [\d,]+ SP\.$/);
  const price = Number(/Cost ([\d,]+) SP/.exec(await cost.textContent())[1].replace(/,/g, ''));
  // A second click takes it back out of the cart.
  await found.click();
  await expect(dlg.locator('#purchase-staged .mech-row')).toHaveCount(0);
  await found.click();
  await dlg.locator('#purchase-buy').click();
  await expect(dlg).toBeHidden();
  await expect(list.locator('.mech-row')).toHaveCount(6);
  await expect.poll(() => warchest(page)).toBe(String(4500 - price));
});

test('recruits a pilot for SP from the warchest', async ({ page }) => {
  await managePage(page, 4500);
  await page.locator('#recruit-open').click();
  const dlg = page.locator('#recruit-dialog');
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#recruit-cost')).toHaveText('Cost 300 SP. Warchest 4,500 → 4,200 SP.');
  await expect(dlg.locator('#recruit-go')).toHaveText('Recruit for 300 SP');
  await dlg.locator('#recruit-name').fill('Jarek');
  await dlg.getByRole('spinbutton', { name: 'Gunnery' }).fill('150');
  await expect(dlg.locator('#recruit-remaining')).toHaveText('0 of 150 SP unallocated');
  await dlg.locator('#recruit-go').click();
  await expect(dlg).toBeHidden();
  await expect(page.locator('li.pilot', { hasText: 'Jarek' })).toBeVisible();
  await expect.poll(() => warchest(page)).toBe('4200');
});

// Applying SP asks first, since an allocation is permanent: Cancel saves nothing, Apply saves it.
test('applying SP asks for a confirmation', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina'], STARTING);
  await page.goto('/refusalwar/manage/');
  const card = page.locator('li.pilot', { hasText: 'Alrek' });
  const id = await card.getAttribute('data-pilot');
  // 250 SP earned, 150 allocated at the start: 100 to spend.
  const res = await page.request.post('/api/pilots/total', { form: { campaign: 'refusalwar', pilot: id, total: '250' } });
  expect(res.ok()).toBeTruthy();
  await page.reload();
  await card.locator('.pilot-sp').click();
  const sp = page.locator('#sp-dialog');
  await sp.locator('.alloc-row input[type=number]').first().fill('150'); // Gunnery from 90 to 150
  await sp.locator('#sp-apply').click();
  const ask = page.getByRole('dialog', { name: 'Spend 60 SP on Alrek?' });
  await expect(ask).toContainText('permanent');
  await ask.getByRole('button', { name: 'Cancel' }).click();
  await expect(sp).toBeVisible();
  await sp.locator('#sp-apply').click();
  await ask.getByRole('button', { name: 'Apply' }).click();
  await expect(card.locator('.pilot-stat.unalloc')).toContainText('40');
});

// More, Dismiss: after a confirmation the pilot leaves the force for good (no memorial).
test('dismisses a pilot from the More menu', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina'], STARTING);
  await page.goto('/refusalwar/manage/');
  const card = page.locator('li.pilot', { hasText: 'Katarina' });
  await card.locator('details.more > summary').click();
  await card.getByRole('button', { name: 'Dismiss' }).click();
  const ask = page.getByRole('dialog', { name: 'Dismiss Katarina?' });
  await expect(ask).toContainText('don’t go to the memorial');
  await ask.getByRole('button', { name: 'Dismiss pilot' }).click();
  await expect(page.locator('li.pilot', { hasText: 'Katarina' })).toHaveCount(0);
  await expect(page.locator('li.pilot', { hasText: 'Alrek' })).toHaveCount(1);
  await expect(page.getByText('Memorial')).toHaveCount(0);
});

// The Purchase dialog is kept in the URL: a reload opens it again with its search (the query and the faction box) and its cart; closing it clears that.
test('a reload brings the Purchase dialog back as it was', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina'], STARTING);
  await page.goto('/refusalwar/manage/');
  await page.locator('#purchase-open').click();
  const dlg = page.locator('#purchase-dialog');
  await dlg.locator('#purchase-search').fill('Timber Wolf A');
  await dlg.locator('#purchase-results .mech-row', { hasText: 'Timber Wolf' }).first().click();
  await dlg.locator('#purchase-search').fill('Fire Moth');
  await dlg.locator('#purchase-available').uncheck();
  await expect(dlg.locator('#purchase-staged .mech-row')).toHaveCount(1);
  await expect(page).toHaveURL(/#purchase=/);

  await page.reload();
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#purchase-search')).toHaveValue('Fire Moth');
  await expect(dlg.locator('#purchase-available')).not.toBeChecked();
  await expect(dlg.locator('#purchase-staged .mech-row')).toHaveCount(1);
  await expect(dlg.locator('#purchase-staged')).toContainText('Timber Wolf');
  await expect(dlg.locator('#purchase-results .mech-row').first()).toContainText('Fire Moth');

  await dlg.locator('#purchase-cancel').click();
  await expect(page).not.toHaveURL(/#purchase=/);
  await page.reload();
  await expect(dlg).toBeHidden();
});
