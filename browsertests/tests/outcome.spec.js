// The Complete mission dialog without a battle (outcome.js), on the test server's Drill campaign: an Aces-style mission whose dialog opens from the page, with
// an Active, a Kills and a Killed in action box per pilot, the mission's objective, the MVP and Commit (confirm.js). Then the campaign History's Roll back
// (campaignhistory.js) undoes it.
const { test, expect, newAccount, startCampaign, recruit } = require('./helpers');

test('reports a mission with a benched and a killed pilot, then rolls it back', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'drill', ['Alrek', 'Katarina']);
  await recruit(page, 'drill', 'Jarek'); // 300 SP from the 5,000 SP warchest

  await page.goto('/drill/scenario_000/');
  await page.locator('#outcome-open').click();
  const out = page.locator('#outcome-dialog');
  await expect(out).toBeVisible();
  const card = (name) => out.locator('.outcome-pilot', { hasText: name });
  await expect(out.locator('.outcome-pilot')).toHaveCount(3);
  for (const name of ['Alrek', 'Katarina', 'Jarek']) await expect(card(name).getByLabel('Active')).toBeChecked();

  // Jarek sits this one out: unticking Active hides his other boxes.
  await card('Jarek').getByLabel('Active').uncheck();
  await expect(card('Jarek').getByLabel('Killed in action')).toBeHidden();
  // Katarina is killed in action.
  await card('Katarina').getByLabel('Killed in action').check();
  await out.getByLabel('Destroy the depot').check();
  // Every active pilot reports their kills; the result waits for them.
  await card('Alrek').getByLabel('Kills').fill('2');
  await card('Katarina').getByLabel('Kills').fill('0');

  // The preview: the objective fills the pool; Alrek takes 80 SP, Jarek half for being benched, Katarina nothing; the rest goes to the warchest.
  await expect(card('Alrek').locator('.outcome-preview')).toContainText('80');
  await expect(card('Jarek').locator('.outcome-preview')).toContainText('40');
  await expect(card('Katarina').locator('.outcome-preview')).toContainText('Killed in Action');
  await expect(out.locator('#outcome-total')).toContainText('Awarded400 SP');
  await expect(out.locator('#outcome-total')).toContainText('Assigned to pilots120 SP');
  await expect(out.locator('#outcome-total')).toContainText('Warchest4700 → 4980 SP');

  // Only a pilot who took part and lives can be the MVP, and one has to be chosen.
  await expect(out.locator('#outcome-mvp option:not([disabled])')).toHaveText(['Alrek']);
  await expect(out.locator('#outcome-commit')).toBeDisabled();
  await out.locator('#outcome-mvp').selectOption({ label: 'Alrek' });
  await expect(out.locator('#outcome-commit')).toBeEnabled();

  // Cancelling the confirmation keeps the dialog open; confirming commits.
  await out.locator('#outcome-commit').click();
  const confirm = page.getByRole('dialog', { name: 'Commit this outcome?' });
  await confirm.getByRole('button', { name: 'Cancel' }).click();
  await expect(confirm).toBeHidden();
  await expect(out).toBeVisible();
  await out.locator('#outcome-commit').click();
  await confirm.getByRole('button', { name: 'Commit' }).click();

  await expect(page.locator('#mission-log-state')).toHaveText('Completed');
  const summary = page.locator('#outcome-summary');
  await expect(summary).toContainText('MVP');
  await expect(summary).toContainText('benched');
  await expect(summary).toContainText('Killed in Action.');
  await expect(summary).toContainText('Keywords granted: Depot Gone');

  // Alrek's pilot card counts the mission he was MVP of.
  await page.goto('/drill/manage/');
  await expect(page.locator('.pilot', { hasText: 'Alrek' }).locator('.pilot-mvp')).toHaveText(/MVP\s*1/);

  // History: roll back to the start (the oldest line with a checkpoint), through two confirmations.
  await page.goto('/drill/');
  await expect(page.locator('.roster-pilot')).toHaveCount(2); // Katarina has fallen
  const history = page.locator('#history-dialog');
  // The Danger zone's Rollback opens the same dialog with only the lines it can roll back to (the start and the completed mission), without the filters.
  await page.locator('#danger-zone summary').click();
  await page.locator('#rollback-open').click();
  await expect(history).toBeVisible();
  await expect(history.locator('#history-title')).toHaveText('Rollback points');
  await expect(history.locator('.history-filters')).toBeHidden();
  await expect(history.locator('.history-entry:visible')).toHaveCount(2);
  await expect(history.locator('.history-entry:visible .history-rollback')).toHaveCount(2);
  await history.locator('#history-close').click();
  // History itself still shows every line, with its filters.
  await page.locator('#history-open').click();
  await expect(history.locator('#history-title')).toHaveText('Campaign history');
  await expect(history.locator('.history-filters')).toBeVisible();
  expect(await history.locator('.history-entry:visible').count()).toBeGreaterThan(2);
  const rollbacks = history.locator('.history-rollback');
  await expect(rollbacks).toHaveCount(2); // the start and the completed mission
  await rollbacks.last().click();
  const first = page.getByRole('dialog', { name: 'Roll back the campaign?' });
  await first.getByRole('button', { name: 'Roll back' }).click();
  const second = page.getByRole('dialog', { name: 'Are you sure?' });
  await second.getByRole('button', { name: 'Yes, roll back' }).click();

  // The page reloads as it was just after the start: two pilots, Katarina among them, and the mission open again.
  await expect(page.locator('.roster-pilot')).toHaveText([/Alrek/, /Katarina/]);
  await expect(page.locator('#scenario-cards .card').first()).toContainText('Open');
});

test('keeps what was entered in the URL, so a reload reopens the dialog with it', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'drill', ['Alrek', 'Katarina']);
  await recruit(page, 'drill', 'Jarek');

  await page.goto('/drill/scenario_000/');
  await expect(page).not.toHaveURL(/#outcome=/); // nothing in the URL before the dialog opens
  await page.locator('#outcome-open').click();
  const out = page.locator('#outcome-dialog');
  const card = (name) => out.locator('.outcome-pilot', { hasText: name });
  await expect(out).toBeVisible();
  await card('Jarek').getByLabel('Active').uncheck();
  await card('Katarina').getByLabel('Killed in action').check();
  await card('Alrek').getByLabel('Kills').fill('3');
  await card('Katarina').getByLabel('Kills').fill('1');
  await out.getByLabel('Destroy the depot').check();
  await out.locator('#outcome-mvp').selectOption({ label: 'Alrek' });
  await expect(out.locator('#outcome-commit')).toBeEnabled();
  await expect(page).toHaveURL(/#outcome=/);

  // A reload opens the dialog again with all of it, and the result worked out from it.
  await page.reload();
  await expect(out).toBeVisible();
  await expect(card('Jarek').getByLabel('Active')).not.toBeChecked();
  await expect(card('Jarek').getByLabel('Killed in action')).toBeHidden();
  await expect(card('Alrek').getByLabel('Active')).toBeChecked();
  await expect(card('Katarina').getByLabel('Killed in action')).toBeChecked();
  await expect(card('Alrek').getByLabel('Killed in action')).not.toBeChecked();
  await expect(card('Alrek').getByLabel('Kills')).toHaveValue('3');
  await expect(card('Katarina').getByLabel('Kills')).toHaveValue('1');
  await expect(out.getByLabel('Destroy the depot')).toBeChecked();
  await expect(out.locator('#outcome-mvp')).toHaveValue(/.+/);
  await expect(out.locator('#outcome-mvp option:checked')).toHaveText('Alrek');
  await expect(out.locator('#outcome-total')).toContainText('Warchest4700 → 4980 SP');
  await expect(out.locator('#outcome-commit')).toBeEnabled();

  // Closing takes it out of the URL, so a reload leaves the dialog closed; opening it again starts afresh.
  await out.locator('#outcome-cancel').click();
  await expect(out).toBeHidden();
  await expect(page).not.toHaveURL(/#outcome=/);
  await page.reload();
  await expect(page.locator('#outcome-open')).toBeVisible();
  await expect(out).toBeHidden();

  // A value out of range or for an input or pilot that doesn't exist is dropped; the rest is kept.
  const pilotIds = await out.evaluate((d) => JSON.parse(d.dataset.pilots).map((p) => p.id));
  const bad = { v: 1, p: { [pilotIds[0]]: { a: 1, k: 0, v: { kills: 9, nope: 1 } }, ghost: { a: 0 } }, m: { depot: 1, nope: 2 }, mvp: 'ghost' };
  const frag = Buffer.from(JSON.stringify(bad)).toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  await page.goto('/drill/'); // from another page: going to the same one with only a new fragment wouldn't load it again
  await page.goto('/drill/scenario_000/#outcome=' + frag);
  await expect(out).toBeVisible();
  await expect(out.getByLabel('Destroy the depot')).toBeChecked();
  await expect(out.locator('.outcome-pilot').first().getByLabel('Kills')).toHaveValue('');
  await expect(out.locator('#outcome-mvp')).toHaveValue('');

  // Committing leaves nothing behind: the page reloads on the completed mission, without the dialog.
  await card('Alrek').getByLabel('Kills').fill('3');
  await card('Katarina').getByLabel('Kills').fill('1');
  await card('Jarek').getByLabel('Kills').fill('0');
  await out.locator('#outcome-mvp').selectOption({ label: 'Alrek' });
  await out.locator('#outcome-commit').click();
  await page.getByRole('dialog', { name: 'Commit this outcome?' }).getByRole('button', { name: 'Commit' }).click();
  await expect(page.locator('#mission-log-state')).toHaveText('Completed');
  await expect(page).not.toHaveURL(/#outcome=/);
  await page.reload();
  await expect(page.locator('#mission-log-state')).toHaveText('Completed');
  await expect(page.locator('#outcome-dialog')).toHaveCount(0);

  // A fragment for a mission that can no longer be completed is dropped.
  await page.goto('/drill/'); // from another page: going to the same one with only a new fragment wouldn't load it again
  await page.goto('/drill/scenario_000/#outcome=' + frag);
  await expect(page.locator('#mission-log-state')).toHaveText('Completed');
  await expect(page).not.toHaveURL(/#outcome=/);
});
