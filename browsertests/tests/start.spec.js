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
  // A started campaign leaves nothing in the URL to come back to, and drops a wizard fragment left over (a bookmark).
  await expect(page).not.toHaveURL(/#start=/);
  await expect(page.locator('#start-wizard')).toHaveCount(0);
  await page.goto('/refusalwar/#start=' + Buffer.from(JSON.stringify({ v: 1, s: 1, f: 'Old' })).toString('base64url'));
  await page.reload(); // a fragment-only change doesn't load the page again
  await expect(page.locator('#force-overview h2')).toHaveText('Wolf Spiders');
  await expect(page).not.toHaveURL(/#start=/);
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
  // A reload keeps the link, and reads the hangar again.
  await page.reload();
  await expect(wiz.locator('#wiz-title')).toHaveText('Link your hangar');
  await expect(link).toHaveValue(HANGAR_LINK);
  await expect(wiz.locator('.hangar-check .hc-summary')).toContainText('5 mechs');
  await wiz.locator('#wiz-next').click();
  await expect(page.locator('#force-overview h2')).toHaveText('Wolf Spiders');
  await expect(page.locator('#force-units .force-summary')).toContainText('5 units');
});

// The wizard is kept in the URL's fragment, so a reload brings the player back where they were: the step, the force, the player count, each pilot (name,
// portrait, SP, Edge abilities), the mechs picked and the unit search. Closing the wizard clears it.
test('a reload brings the start wizard back where it was', async ({ page }) => {
  await newAccount(page);
  await page.goto('/refusalwar/');
  await page.locator('#wizard-open').click();
  const wiz = page.locator('#start-wizard');
  const next = wiz.locator('#wiz-next');
  await wiz.locator('#wiz-force').fill('Wolf Spiders');
  await next.click();
  await wiz.getByRole('button', { name: '2 players' }).click();
  await next.click();
  await pilotStep(page, 'Alrek');
  // Katarina: 60 SP to Edge abilities earns her one, chosen here.
  await wiz.locator('#wiz-name').fill('Katarina');
  const portrait = await wiz.locator('.portrait-btn img').getAttribute('src');
  await wiz.getByRole('spinbutton', { name: 'Gunnery' }).fill('90');
  await wiz.getByRole('spinbutton', { name: 'Edge abilities' }).fill('60');
  const ability = wiz.locator('#wiz-ability-0');
  await ability.selectOption({ index: 1 });
  const chosen = await ability.inputValue();
  expect(chosen).not.toBe('');
  await next.click();
  // The hangar: two Grizzlies picked, and the search left on the Kit Fox with the faction box unticked.
  await expect(wiz.locator('#wiz-title')).toHaveText('Build your force');
  const search = wiz.locator('#wiz-units-search');
  const found = (name) => wiz.locator('.purchase-found .mech-row', { hasText: name }).first();
  await search.fill('Grizzly');
  await found('Grizzly').click();
  await found('Grizzly').click();
  await expect(found('Grizzly')).toContainText('2 in force');
  await search.fill('Kit Fox');
  await wiz.locator('#wiz-units-available').uncheck();
  await expect(found('Kit Fox')).toBeVisible();
  await expect(page).toHaveURL(/#start=/);

  await page.reload();
  await expect(wiz).toBeVisible();
  await expect(wiz.locator('#wiz-count')).toHaveText('Step 5 of 5');
  await expect(wiz.locator('#wiz-title')).toHaveText('Build your force');
  await expect(wiz.locator('.wiz-pilot-cards')).toContainText('Alrek');
  await expect(wiz.locator('.wiz-pilot-cards')).toContainText('Katarina');
  await expect(wiz.locator('.purchase-staged .mech-row')).toHaveCount(2);
  await expect(wiz.locator('.purchase-staged')).toContainText('Grizzly');
  await expect(search).toHaveValue('Kit Fox');
  await expect(wiz.locator('#wiz-units-available')).not.toBeChecked();
  await expect(found('Kit Fox')).toBeVisible();
  await search.fill('Grizzly');
  await expect(found('Grizzly')).toContainText('2 in force');

  // Back through the steps: every value is still there.
  await wiz.locator('#wiz-back').click();
  await expect(wiz.locator('#wiz-name')).toHaveValue('Katarina');
  await expect(wiz.locator('.portrait-btn img')).toHaveAttribute('src', portrait);
  await expect(wiz.getByRole('spinbutton', { name: 'Edge abilities' })).toHaveValue('60');
  await expect(ability).toHaveValue(chosen);
  await expect(wiz.locator('.alloc-remaining')).toHaveText('0 of 150 SP left to allocate');
  await wiz.locator('#wiz-back').click();
  await expect(wiz.locator('#wiz-name')).toHaveValue('Alrek');
  await wiz.locator('#wiz-back').click();
  await expect(wiz.getByRole('button', { name: '2 players' })).toHaveAttribute('aria-pressed', 'true');
  await wiz.locator('#wiz-back').click();
  await expect(wiz.locator('#wiz-force')).toHaveValue('Wolf Spiders');

  // Closing the wizard takes it out of the URL.
  await wiz.locator('#wiz-cancel').click();
  await expect(page).not.toHaveURL(/#start=/);
  await page.reload();
  await expect(wiz).toBeHidden();
});

// A fragment is checked before it is put back: what no longer holds goes, and the wizard opens on the first step still missing something.
test('a restored start wizard keeps only what still holds', async ({ page }) => {
  await newAccount(page);
  const state = {
    v: 1, s: 4, f: 'Wolf Spiders', n: 9,
    p: [
      { n: 'Alrek', i: 'no-such-portrait.png', a: [90, 0, 60, 0], e: [] },
      { n: 'Katarina', i: '', a: [150, 150, 0, 0], e: ['no_such_ability'] }
    ],
    m: 'b', u: [['BMGrizzly', 1], ['BMNoSuchMech', 2]], q: 'Grizzly'
  };
  await page.goto('/refusalwar/#start=' + Buffer.from(JSON.stringify(state)).toString('base64url'));
  const wiz = page.locator('#start-wizard');
  await expect(wiz).toBeVisible();
  // Katarina's 300 SP were more than her budget: she keeps the first 150 (in Gunnery), and her step is whole again, so the wizard opens on the hangar.
  await expect(wiz.locator('#wiz-count')).toHaveText('Step 5 of 5');
  // The unknown mech is left out; the search is back.
  await expect(wiz.locator('.purchase-staged .mech-row')).toHaveCount(1);
  await expect(wiz.locator('#wiz-units-search')).toHaveValue('Grizzly');
  await wiz.locator('#wiz-back').click();
  await expect(wiz.locator('#wiz-name')).toHaveValue('Katarina');
  await expect(wiz.getByRole('spinbutton', { name: 'Gunnery' })).toHaveValue('150');
  await expect(wiz.getByRole('spinbutton', { name: 'Piloting' })).toHaveValue('0');
  await wiz.locator('#wiz-back').click();
  await expect(wiz.locator('#wiz-name')).toHaveValue('Alrek');
  await expect(wiz.locator('.portrait-btn img')).toHaveAttribute('src', /^\/portraits\/.+/);
  await expect(wiz.locator('.portrait-btn img')).not.toHaveAttribute('src', /no-such-portrait/);
  await wiz.locator('#wiz-back').click();
  // 9 players is out of range: the campaign's most (as the Deploy wizard's restore does).
  await expect(wiz.getByRole('button', { name: '2 players' })).toHaveAttribute('aria-pressed', 'true');

  // A step that isn't finished stops the wizard there: Alrek's SP no longer all allocated.
  state.p[0].a = [90, 0, 0, 0];
  await page.goto('/refusalwar/#start=' + Buffer.from(JSON.stringify(state)).toString('base64url'));
  await page.reload(); // a fragment-only change doesn't load the page again
  await expect(wiz.locator('#wiz-count')).toHaveText('Step 3 of 5');
  await expect(wiz.locator('.alloc-remaining')).toHaveText('60 of 150 SP left to allocate');

  // Something that isn't a wizard state is dropped.
  await page.goto('/refusalwar/#start=garbage');
  await page.reload();
  await expect(wiz).toBeHidden();
  await expect(page).not.toHaveURL(/#start=/);
});
