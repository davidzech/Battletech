// The start wizard (campaign.js): name the force, the player count, a step per starting pilot (name and SP allocation), then the hangar, built from the
// unit search (unitsearch.js) or copied from a MekBay link.
const { test, expect, newAccount, setHangar, STARTING, HANGAR_LINK } = require('./helpers');

async function pilotStep(page, name) {
  const wiz = page.locator('#start-wizard');
  await expect(wiz.locator('#wiz-title')).toContainText('Named pilot');
  const next = wiz.locator('#wiz-next');
  await expect(next).toBeDisabled();
  await wiz.locator('#wiz-name').fill(name);
  // Every point has to go somewhere: Next waits until all 150 SP are allocated.
  await wiz.getByRole('spinbutton', { name: 'Gunnery' }).fill('90');
  await expect(wiz.locator('.alloc-remaining')).toHaveText('60 of 150 SP left to allocate');
  await expect(next).toBeDisabled();
  await expect(wiz.locator('#wiz-hint')).toContainText('60 left');
  await wiz.getByRole('spinbutton', { name: 'Edge tokens' }).fill('60');
  await expect(wiz.locator('.alloc-remaining')).toHaveText('0 of 150 SP left to allocate');
  await expect(next).toBeEnabled();
  await next.click();
}

async function nameAndPilots(page) {
  await page.goto('/refusalwar/');
  await page.locator('#wizard-open').click();
  const wiz = page.locator('#start-wizard');
  await expect(wiz).toBeVisible();
  await expect(wiz.locator('#wiz-count')).toHaveText('Step 1 of 5');
  await expect(wiz.locator('#wiz-next')).toBeDisabled();
  await wiz.locator('#wiz-force').fill('Wolf Spiders');
  await wiz.locator('#wiz-next').click();

  // Refusal War is for one or two players.
  await expect(wiz.locator('#wiz-title')).toHaveText('How many players?');
  await wiz.getByRole('button', { name: '2 players' }).click();
  await expect(wiz.getByRole('button', { name: '2 players' })).toHaveAttribute('aria-pressed', 'true');
  await expect(wiz.getByRole('button', { name: '1 player', exact: true })).toHaveAttribute('aria-pressed', 'false');
  await wiz.locator('#wiz-next').click();

  await pilotStep(page, 'Alrek');
  await pilotStep(page, 'Katarina');
  await expect(wiz.locator('#wiz-count')).toHaveText('Step 5 of 5');
  await expect(wiz.locator('#wiz-next')).toHaveText('Start campaign');
  return wiz;
}

test('builds a starting force from the unit search', async ({ page }) => {
  await newAccount(page);
  const wiz = await nameAndPilots(page);
  await expect(wiz.locator('#wiz-title')).toHaveText('Build your force');
  await expect(wiz.locator('.wiz-pilot-cards')).toContainText('Alrek');

  // Five mechs: each click on a result adds another of it, a right-click takes one back.
  const search = wiz.locator('#wiz-units-search');
  const found = (name) => wiz.locator('.purchase-found .mech-row', { hasText: name }).first();
  const pick = async (name, times) => {
    await search.fill(name);
    await expect(found(name)).toBeVisible();
    for (let i = 0; i < times; i++) await found(name).click();
  };
  await pick('Grizzly', 1);
  await expect(found('Grizzly')).toContainText('In force');
  await pick('Mist Lynx', 3);
  await expect(found('Mist Lynx')).toContainText('3 in force');
  await found('Mist Lynx').click({ button: 'right' });
  await expect(found('Mist Lynx')).toContainText('2 in force');
  await expect(wiz.locator('#wiz-next')).toBeDisabled();
  await expect(wiz.locator('.hc-summary')).toContainText('3 mechs (at least 5)');
  await pick('Kit Fox', 2);
  await expect(wiz.locator('.purchase-staged .mech-row')).toHaveCount(5);
  await expect(wiz.locator('#wiz-next')).toBeEnabled();

  // The remove button on a picked mech takes it off again.
  await wiz.locator('.purchase-staged .mech-row').first().getByRole('button', { name: 'Remove' }).click();
  await expect(wiz.locator('.purchase-staged .mech-row')).toHaveCount(4);
  await expect(wiz.locator('#wiz-next')).toBeDisabled();
  await pick('Grizzly', 1);
  await expect(wiz.locator('.purchase-staged .mech-row')).toHaveCount(5);

  await wiz.locator('#wiz-next').click();
  // The page reloads with the started campaign: the force, its pilots and the mechs picked.
  await expect(page.locator('#force-overview h2')).toHaveText('Wolf Spiders');
  await expect(page.locator('.roster-pilot')).toHaveCount(2);
  await expect(page.locator('#force-units .force-summary')).toContainText('5 units');
  await expect(page.locator('#force-units img[alt^="Grizzly"]')).toHaveCount(1);
  await expect(page.locator('#force-units img[alt^="Mist Lynx"]')).toHaveCount(2);
});

test('copies a starting force from a MekBay link', async ({ page }) => {
  await newAccount(page);
  await setHangar(page, STARTING);
  const wiz = await nameAndPilots(page);
  await wiz.getByRole('button', { name: 'Use a MekBay link' }).click();
  await expect(wiz.locator('#wiz-title')).toHaveText('Link your hangar');
  const link = wiz.locator('#wiz-hangar');
  await link.fill('not a link');
  await expect(wiz.locator('#wiz-hint')).toContainText('Paste a MekBay hangar link');
  // A pasted link is cut down to the hangar, and the hangar is read and checked against the starting conditions.
  await link.fill(HANGAR_LINK + '&sel=01a12f9d-a79b-717d-8c84-d5da73d2f9a5');
  await expect(link).toHaveValue(HANGAR_LINK);
  await expect(wiz.locator('.hangar-check .hc-summary')).toContainText('5 mechs');
  await expect(wiz.locator('.hangar-check .hc-units .mech-row')).toHaveCount(5);
  await wiz.locator('#wiz-next').click();
  await expect(page.locator('#force-overview h2')).toHaveText('Wolf Spiders');
  await expect(page.locator('#force-units .force-summary')).toContainText('5 units');
});
