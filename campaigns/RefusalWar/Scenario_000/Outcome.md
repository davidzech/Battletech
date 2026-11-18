---
id: scenario_000
default_next: scenario_001
inputs:
  - id: kills
    label: Kills
    max: 4
  - id: killed
    label: Killed in action
    type: bool
outcomes:
  - when: kills == 1 and not killed
    sp: 150
  - when: kills == 2 and not killed
    sp: 400
  - when: kills == 3 and not killed
    sp: 650
  - when: kills >= 4 and not killed
    sp: 900
  - when: killed or kills == 0
    kia: true
    free_recruits: 1
    label: "Dies or fails to kill: permanently removed. A replacement pilot can be recruited free."
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

If your pilot dies, or fails to kill at least 1 opponent, they are permanently removed from the campaign. You will receive a new named pilot with 150 starting SP for Sortie 001.
