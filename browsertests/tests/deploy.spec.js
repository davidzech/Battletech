// The Deploy wizard (mekbay.js) on Refusal War's Trial of Position: the scenario provides its own omnimechs (two at most, each with a named pilot) and lets
// them be reconfigured for free, so the wizard has the pilot tray, the unit limit, the Configure step and Start battle; then the Operation in progress view,
// the Complete mission dialog reading the battle (outcome.js) and Abort mission.
const { test, expect, newAccount, startCampaign, setBattle, recruit } = require('./helpers');

const SCENARIO = '/refusalwar/scenario_000/';

// The battle the fake MekBay reports: Alrek and Katarina each crew a mech, and Katarina's record sheet shows her dead.
async function battleWithLosses(page) {
  await setBattle(page, {
    Forces: [{
      InstanceID: 'p1', Alignment: 'friendly',
      Force: {
        Name: 'Player 1', Units: [
          { ID: 'b1', Unit: 'BMBlackHawk_Prime', Group: 'Deployed', Pilot: 'Alrek', Gunnery: 3, Piloting: 5 },
          { ID: 'b2', Unit: 'BMRyoken_Prime', Group: 'Deployed', Pilot: 'Katarina', Gunnery: 3, Piloting: 5, CrewDead: true, Damaged: true }
        ]
      }
    }]
  });
}

async function openDeploy(page) {
  await page.goto(SCENARIO);
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#deploy-list .deploy-unit')).toHaveCount(5);
  return dlg;
}

test('deploys two pilots, reconfigures a mech, starts the battle and completes the mission', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  await battleWithLosses(page);
  const dlg = await openDeploy(page);
  const rows = dlg.locator('#deploy-list .deploy-unit');
  const tray = dlg.locator('#deploy-pilots-list .deploy-pilot');
  await expect(tray).toHaveCount(2);
  await expect(dlg.locator('#deploy-rules')).toContainText('Take up to 2 mechs in all, every mech needs a named pilot.');
  await expect(dlg.locator('#deploy-next')).toBeDisabled();

  // Click a pilot, then a mech's slot: the pilot goes in and the mech is taken.
  await tray.filter({ hasText: 'Alrek' }).click();
  await expect(tray.filter({ hasText: 'Alrek' })).toHaveAttribute('aria-pressed', 'true');
  await rows.nth(0).locator('.deploy-slot').click();
  await expect(rows.nth(0).locator('input[type=checkbox]')).toBeChecked();
  await expect(rows.nth(0).locator('.deploy-slot')).toContainText('Alrek');
  await expect(tray.filter({ hasText: 'Alrek' })).toContainText('in ');

  // Drag the other pilot onto a second mech.
  await tray.filter({ hasText: 'Katarina' }).dragTo(rows.nth(1).locator('.deploy-slot'));
  await expect(rows.nth(1).locator('.deploy-slot')).toContainText('Katarina');
  await expect(rows.nth(1).locator('input[type=checkbox]')).toBeChecked();

  // Two mechs is the scenario's limit: the rest can't be taken.
  await expect(rows.nth(2).locator('input[type=checkbox]')).toBeDisabled();
  await expect(dlg.locator('#deploy-meter')).toContainText('2 / 2 mechs');

  // The x on a slot takes the pilot back out; a mech without a pilot holds Next back.
  await rows.nth(1).getByRole('button', { name: /Take Katarina out/ }).click();
  await expect(rows.nth(1).locator('.deploy-slot')).not.toContainText('Katarina');
  await expect(dlg.locator('#deploy-next')).toBeDisabled();
  await tray.filter({ hasText: 'Katarina' }).click();
  await rows.nth(1).locator('.deploy-slot').click();
  await expect(dlg.locator('#deploy-next')).toBeEnabled();
  await dlg.locator('#deploy-next').click();

  // The Configure step: a free variant for each omnimech taken.
  await expect(dlg.locator('#deploy-step')).toHaveText('Configure your mechs');
  const picks = dlg.locator('.variant-pick');
  await expect(picks).toHaveCount(2);
  const select = picks.first().locator('select');
  const options = await select.locator('option:not([disabled])').evaluateAll((os) => os.map((o) => o.value).filter(Boolean));
  expect(options.length).toBeGreaterThan(0);
  await select.selectOption(options[0]);
  await expect(picks.first()).toContainText('Free');

  await expect(dlg.locator('#deploy-go')).toBeEnabled();
  await dlg.locator('#deploy-go').click();

  // The Operation in progress view: the table's link and the battle as MekBay has it, with Katarina dead on her record sheet.
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');
  await expect(dlg.locator('.battle-link-url')).toContainText('mekbay-');
  await expect(dlg.locator('.op-force')).toHaveCount(1);
  await expect(dlg.locator('.op-force .deploy-unit')).toHaveCount(2);
  await expect(dlg.locator('.op-force .kia-stamp')).toHaveCount(1);

  // Complete mission: the dialog reads who took part and who died from the battle.
  await dlg.locator('#outcome-open').click();
  const out = page.locator('#outcome-dialog');
  await expect(out).toBeVisible();
  const alrek = out.locator('.outcome-pilot', { hasText: 'Alrek' });
  const katarina = out.locator('.outcome-pilot', { hasText: 'Katarina' });
  await expect(alrek.locator('.outcome-took')).toHaveText('Took part');
  await expect(katarina.locator('.outcome-took')).toContainText('dead on the record sheet');
  await expect(katarina.getByLabel('Killed in action')).toBeChecked();
  await expect(alrek.getByLabel('Killed in action')).not.toBeChecked();
  await alrek.getByLabel('Kills').fill('2');
  await katarina.getByLabel('Kills').fill('1');
  // Nothing can be committed until the MVP is chosen; only Alrek, alive, can be it.
  await expect(out.locator('.outcome-mvp-needed')).toBeVisible();
  await expect(out.locator('#outcome-commit')).toBeDisabled();
  await expect(out.locator('#outcome-mvp option:not([disabled])')).toHaveText(['Alrek']);
  await out.locator('#outcome-mvp').selectOption({ label: 'Alrek' });
  await expect(alrek.locator('.outcome-preview')).toContainText('400');
  await expect(katarina.locator('.outcome-preview')).toContainText('Killed in Action');
  await expect(out.locator('#outcome-commit')).toBeEnabled();
  await out.locator('#outcome-commit').click();

  // Committing asks first (confirm.js), then the page reloads with the mission completed.
  const confirm = page.getByRole('dialog', { name: 'Commit this outcome?' });
  await expect(confirm).toBeVisible();
  await confirm.getByRole('button', { name: 'Commit' }).click();
  await expect(page.locator('#mission-log-state')).toHaveText('Completed');
  await expect(page.locator('#outcome-summary')).toContainText('Alrek');
  await expect(page.locator('#outcome-summary')).toContainText('Killed in Action');
});

test('aborts a battle and rolls the campaign back', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  // A recruit after the start, which the rollback undoes.
  await recruit(page, 'refusalwar', 'Jarek');
  await battleWithLosses(page);
  const dlg = await openDeploy(page);
  const rows = dlg.locator('#deploy-list .deploy-unit');
  // Ticking a mech's box takes it; the slot then waits for its pilot.
  await rows.nth(0).locator('input[type=checkbox]').check();
  await expect(rows.nth(0)).toHaveClass(/picked/);
  await expect(dlg.locator('#deploy-rules')).toContainText('Assign a named pilot to each mech you take.');
  await dlg.locator('#deploy-pilots-list .deploy-pilot', { hasText: 'Alrek' }).dragTo(rows.nth(0).locator('.deploy-slot'));
  await dlg.locator('#deploy-next').click();
  // With one mech there is nobody else to configure for, but the step is still offered for its omnimech: keep its own variant.
  await expect(dlg.locator('#deploy-step')).toHaveText('Configure your mechs');
  await dlg.locator('#deploy-go').click();
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');

  // Abort mission is under More options; the choice is its own dialog.
  await dlg.locator('#op-abort summary').click();
  await dlg.locator('#abort-open').click();
  const abort = page.getByRole('dialog', { name: 'Abort mission' });
  await expect(abort).toBeVisible();
  await abort.getByRole('button', { name: 'Keep playing' }).click();
  await expect(abort).toBeHidden();
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');
  await dlg.locator('#abort-open').click();
  const reloaded = page.waitForEvent('load');
  await abort.getByRole('button', { name: 'Roll back' }).click();

  // The page reloads with the mission not started again.
  await reloaded;
  await expect(page.locator('#deploy-open')).toHaveText('Deploy force');
  await expect(page.locator('#mission-log-state')).toHaveText('Not completed');
  await page.goto('/refusalwar/');
  await expect(page.locator('.roster-pilot')).toHaveText([/Alrek/, /Katarina/]);
});

test('divides the force between two players and reviews the plan', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  await battleWithLosses(page);
  const dlg = await openDeploy(page);
  const pick = dlg.locator('#deploy-players-pick');
  await expect(pick.getByRole('button', { name: '1 player', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await pick.getByRole('button', { name: '2 players' }).click();
  await expect(pick.getByRole('button', { name: '2 players' })).toHaveAttribute('aria-pressed', 'true');
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 1 of 2');

  const rows = dlg.locator('#deploy-list .deploy-unit');
  const tray = dlg.locator('#deploy-pilots-list .deploy-pilot');
  await tray.filter({ hasText: 'Alrek' }).click();
  await rows.nth(0).locator('.deploy-slot').click();
  // With two players and a limit of two mechs, Player 1 leaves one for Player 2.
  await expect(rows.nth(1).locator('input[type=checkbox]')).toBeDisabled();
  await dlg.locator('#deploy-next').click();

  // Player 2 sees neither the mech nor the pilot Player 1 took.
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 2 of 2');
  await expect(rows).toHaveCount(4);
  await expect(tray).toHaveCount(1);
  await tray.click();
  await rows.nth(0).locator('.deploy-slot').click();
  await dlg.locator('#deploy-next').click();

  await expect(dlg.locator('#deploy-step')).toHaveText('Configure your mechs');
  await dlg.locator('#deploy-next').click();
  await expect(dlg.locator('#deploy-step')).toHaveText('Review your plan');
  await expect(dlg.locator('.deploy-review-player')).toHaveCount(2);
  await expect(dlg.locator('.deploy-review-player').nth(0)).toContainText('Alrek');
  await expect(dlg.locator('.deploy-review-player').nth(1)).toContainText('Katarina');
  await expect(dlg.locator('#deploy-count')).toHaveText('Step 4 of 4');

  // The review's rows show their unit cards on hover.
  await dlg.locator('.deploy-review .mech-row').first().locator('.mr-name').hover();
  await expect(dlg.locator('#pill-tip .tip-head')).toBeVisible();

  await dlg.locator('#deploy-go').click();
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');
  await dlg.locator('.op-force .mech-row').first().locator('.mr-name').hover();
  await expect(dlg.locator('#pill-tip .tip-head')).toBeVisible();
});
