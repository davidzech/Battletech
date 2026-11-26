---
id: scenario_005
default_next: ""
branches: [scenario_007, scenario_008]   # the December fronts: Twycross or Wotan
pilot_sp: 100   # SP each pilot who took part draws from the pool; the only place the amount is set
objectives:
  - id: defenders_destroyed
    label: "Primary: destroy all defenders"
    sp: 800
    required: true
  - id: radio_operative
    label: "Objective 1: the radio operative was rescued"
    sp: 150
  - id: technician
    label: "Objective 2: the electrical technician was rescued"
    sp: 100
  - id: worker
    label: "Objective 3: the construction worker was rescued"
    sp: 50
  - id: array_commander
    label: "Objective 4: the sensor array's commander was rescued"
    sp: 200
outcomes:
  # Every member of the staff saved earns Savior; two or more killed, Blackout.
  - when: radio_operative and technician and worker and array_commander
    grants: [savior]
  - when: "(radio_operative ? 1 : 0) + (technician ? 1 : 0) + (worker ? 1 : 0) + (array_commander ? 1 : 0) <= 2"
    grants: [blackout]
aftermath:
  - when: radio_operative and technician and worker and array_commander
    text: |
      The sensor array dome is secure. You have driven the enemy from the installation, and the staff is safe.

      On the surface of Devin, a battle rages: Jade Falcon units have engaged us on the black sand of the O'Tsung Plain. The prompt recovery of the sensor array will bolster our units below.

      The Keshik is aware of your actions. Honorable deeds beget praise.
  - when: "(radio_operative ? 1 : 0) + (technician ? 1 : 0) + (worker ? 1 : 0) + (array_commander ? 1 : 0) == 3"
    text: |
      The sensor array dome is secure. You have driven the enemy from the installation, and the staff is safe.

      On the surface of Devin, a battle rages: Jade Falcon units have engaged us on the black sand of the O'Tsung Plain. The Keshik hopes that our temporary loss of the sensor array will not hamper our units below.

      The Keshik is aware of your actions.
  - when: "(radio_operative ? 1 : 0) + (technician ? 1 : 0) + (worker ? 1 : 0) + (array_commander ? 1 : 0) <= 2"
    text: |
      The domes' integrity was compromised, and the colonists were exposed to hard vacuum. There were no survivors.

      The loss of the sensor array has left us in an intelligence blackout. We now know little of Falcon movements in the rear.

      The Keshik is less than content with your performance.
---

# Scenario 005: Sable Flame - Outcome

## Victory & Rewards

Objectives met fill the mission's pool of SP. Each pilot who took part draws a flat share from it (half for a pilot who sat the mission out, and nothing for a pilot killed in action, who is lost); whatever is left goes to the warchest.

| Objective                                                    | SP  |
|--------------------------------------------------------------|-----|
| Primary: destroy all defenders                               | 800 |
| Secondary, Objective 1: rescue the radio operative           | 150 |
| Secondary, Objective 2: rescue the electrical technician     | 100 |
| Secondary, Objective 3: rescue the construction worker       | 50  |
| Secondary, Objective 4: rescue the sensor array's commander  | 200 |

## Keywords

- **Savior** if all four of the staff were rescued.
- **Blackout** if two or more of the staff were killed.
