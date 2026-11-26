---
id: scenario_003
default_next: ""
pilot_sp: 100   # SP each pilot who took part draws from the pool; the only place the amount is set
objectives:
  - id: converter_inspected
    label: "Primary: inspect the power converter"
    sp: 400
    required: true
inputs:
  - id: karyoplasma
    label: The power converter was converting karyoplasma
    scope: mission
    type: bool
  - id: converter_destroyed
    label: The power converter was destroyed
    scope: mission
    type: bool
  - id: commander_scanned
    label: The force commander was destroyed and their mech scanned for intel
    scope: mission
    type: bool
outcomes:
  # The secondary objective depends on what the inspection found: destroy a karyoplasma converter, or else take the intel from the force commander.
  - when: karyoplasma and converter_destroyed
    label: "Secondary: the karyoplasma converter was destroyed"
    pool: 800
  - when: not karyoplasma and commander_scanned
    label: "Secondary: intel on the hidden HQ taken from the force commander's mech"
    pool: 800
  - when: karyoplasma and not converter_destroyed
    grants: [full_power]
aftermath:
  - when: karyoplasma and converter_destroyed
    text: |
      The power converter was inspected and destroyed. The city has become useless to the enemy as a base of operations, and the 2nd Wolf Cavaliers are busy laying waste to the 10th.

      The enemy is vanquished. The Keshik has noted your excellence in the field.
  - when: karyoplasma and not converter_destroyed
    text: |
      You have failed your objective, and the power converter remains. The 2nd Wolf Cavaliers will take over in your stead for a second strike.

      Your actions have disgraced the Clan.
  - when: not karyoplasma and commander_scanned
    text: |
      The location of the hidden Falcon headquarters was found in the data files of the destroyed Star Commander's mech. The 2nd Wolf Cavaliers will find and destroy them.

      The Keshik has noted your excellence in the field.
  - when: not karyoplasma and not commander_scanned
    text: |
      Our intelligence network has failed us, and we have no further leads on the location of the hidden Falcon headquarters.

      The Keshik is displeased.
---

# Scenario 003: Blade Splint - Outcome

## Victory & Rewards

The power converter must have been inspected, or the mission is a failure and is played again.

Objectives met fill the mission's pool of SP. Each pilot who took part draws a flat share from it (half for a pilot who sat the mission out, and nothing for a pilot killed in action, who is lost); whatever is left goes to the warchest.

| Objective                                                                    | SP  |
|------------------------------------------------------------------------------|-----|
| Primary: inspect the power converter                                         | 400 |
| Secondary, if it was converting karyoplasma: destroy it                      | 800 |
| Secondary, if it was not: destroy the force commander and scan their mech    | 800 |

## Keywords

- **Full Power** if the converter was converting karyoplasma and was not destroyed.
