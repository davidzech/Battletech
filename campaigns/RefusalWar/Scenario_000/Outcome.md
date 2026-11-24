---
id: scenario_000
default_next: scenario_001
inputs:
  - id: kills
    label: Kills
    max: 4
outcomes:
  - when: kills == 1 and not killed
    sp: 150
  - when: kills == 2 and not killed
    sp: 400
  - when: kills == 3 and not killed
    sp: 650
  - when: kills >= 4 and not killed
    sp: 900
  - when: killed
    kia: true
    free_recruits: 1
    label: "Killed in Action."
  - when: kills == 0 and not killed
    kia: true
    free_recruits: 1
    label: "Failed Trial of Position with 0 kills. Demoted to Merchant Caste."
  # Every cadet who passes the Trial becomes at least a MechWarrior; the one with the most kills (the MVP, on a tie) a Star Commander.
  - when: kills > 0 and not killed
    promote: MechWarrior
  - when: kills > 0 and not killed and kills == most_kills and (most_kills_pilots == 1 or mvp)
    promote: Star Commander
---

# Scenario 000: Trial of Position - Outcome

## Victory & Rewards

Each named pilot earns the following SP for the number of kills for this scenario.

| Kills | SP  |
|-------|-----|
| 1     | 150 |
| 2     | 400 |
| 3     | 650 |
| 4     | 900 |

Every pilot who passes the Trial is promoted to MechWarrior, and the pilot with the most kills to Star Commander (on a tie, the mission's MVP).

If your pilot dies, or fails to kill at least 1 opponent, they are permanently removed from the campaign. You will receive a new named pilot with 150 starting SP for Scenario 001.
