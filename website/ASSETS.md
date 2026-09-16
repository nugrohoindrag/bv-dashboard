# Website visual assets (Website PRD v1.1 §8)

All photography on the public website is sourced from **Unsplash** under the [Unsplash License](https://unsplash.com/license):
free to use for commercial and non-commercial purposes, no permission needed, attribution appreciated but not required.
Photos may not be sold unaltered or used to replicate a competing service, which this website does not do.

Images are downloaded once by `npm run images` (`scripts/images.mjs`) from the manifest `src/content/images.json`,
resized into responsive WebP variants (480 / 960 / 1440 px) plus a 960 px JPEG fallback, and written to `public/images/`
(committed, so CI builds need no network). A tiny blurred placeholder (LQIP) per image is generated into
`src/content/images.generated.json` and used as the background while the real image loads.

Product screens on the website are lightweight UI mockups rendered from design tokens (`src/components/mocks.tsx`), not
screenshots, so they stay consistent with the product identity (PRD §8.3).

| Key | Unsplash photo | Alt text | Used on |
|---|---|---|---|
| `hero-towers` | [1486406146926-c627a92ad1ab](https://unsplash.com/photos/1486406146926-c627a92ad1ab) | Glass office towers seen from street level | office, home |
| `modern-building` | [1487958449943-2429e8be8625](https://unsplash.com/photos/1487958449943-2429e8be8625) | Modern building facade against a bright sky | about, security |
| `apartment-exterior` | [1545324418-cc1a3fa10c00](https://unsplash.com/photos/1545324418-cc1a3fa10c00) | Apartment building exterior at dusk | apartment |
| `apartment-living` | [1522708323590-d24dbb6b0267](https://unsplash.com/photos/1522708323590-d24dbb6b0267) | Bright apartment living room with a red chair | apartment, tenant-app |
| `apartment-lounge` | [1560448204-e02f11c3d0e2](https://unsplash.com/photos/1560448204-e02f11c3d0e2) | Apartment lounge with large windows | apartment |
| `apartment-kitchen` | [1600607686527-6fb886090705](https://unsplash.com/photos/1600607686527-6fb886090705) | Clean apartment kitchen with bar stools | apartment, housekeeping |
| `house-keys` | [1560518883-ce09059eeffa](https://unsplash.com/photos/1560518883-ce09059eeffa) | House keys on a wooden table | apartment, unit-sales |
| `hotel-pool` | [1566073771259-6a8506099945](https://unsplash.com/photos/1566073771259-6a8506099945) | Hotel pool deck with loungers | hotel |
| `hotel-exterior` | [1551882547-ff40c63fe5fa](https://unsplash.com/photos/1551882547-ff40c63fe5fa) | Hotel exterior with palm trees at sunset | hotel |
| `hotel-room` | [1512918728675-ed5a9ecdebfd](https://unsplash.com/photos/1512918728675-ed5a9ecdebfd) | Hotel bedroom with white linen | hotel, housekeeping |
| `hotel-suite` | [1578683010236-d716f9a3f461](https://unsplash.com/photos/1578683010236-d716f9a3f461) | Hotel suite with a view | hotel |
| `banquet-hall` | [1519167758481-83f550bb49b3](https://unsplash.com/photos/1519167758481-83f550bb49b3) | Hotel banquet hall set for an event | hotel, facility |
| `office-window` | [1497215728101-856f4ea42174](https://unsplash.com/photos/1497215728101-856f4ea42174) | Office workspace by a large window | office |
| `open-office` | [1531973576160-7125cd663d86](https://unsplash.com/photos/1531973576160-7125cd663d86) | Open plan office floor | office, property-operations |
| `office-interior` | [1497366216548-37526070297c](https://unsplash.com/photos/1497366216548-37526070297c) | Office interior with a kitchen area | office |
| `meeting-room` | [1571624436279-b272aff752b5](https://unsplash.com/photos/1571624436279-b272aff752b5) | Meeting room with leather chairs | office, facility, demo |
| `office-team` | [1556761175-b413da4baf72](https://unsplash.com/photos/1556761175-b413da4baf72) | Team working together in an office | office, tenant-relation |
| `cleaning-window` | [1581578731548-c64695cc6952](https://unsplash.com/photos/1581578731548-c64695cc6952) | Housekeeper cleaning a window | housekeeping |
| `vacuum` | [1527515637462-cff94eecc1ac](https://unsplash.com/photos/1527515637462-cff94eecc1ac) | Vacuum cleaner on a carpet | housekeeping |
| `clean-bathroom` | [1584622650111-993a426fbf0a](https://unsplash.com/photos/1584622650111-993a426fbf0a) | Spotless bathroom | housekeeping |
| `electrician` | [1621905251189-08b45d6a269e](https://unsplash.com/photos/1621905251189-08b45d6a269e) | Electrician in a hard hat working on a panel | engineering, work-orders |
| `technician` | [1558618666-fcd25c85cd64](https://unsplash.com/photos/1558618666-fcd25c85cd64) | Technician inspecting equipment with a headlamp | engineering, mobile-staff |
| `engineer-plans` | [1581092160562-40aa08e78837](https://unsplash.com/photos/1581092160562-40aa08e78837) | Engineer reviewing technical drawings | engineering, asset-management |
| `server-room` | [1573164713988-8665fc963095](https://unsplash.com/photos/1573164713988-8665fc963095) | Technician in a server room | asset-management, security |
| `cctv` | [1557597774-9d273605dfa9](https://unsplash.com/photos/1557597774-9d273605dfa9) | Wall of security cameras | security |
| `property-manager` | [1590650153855-d9e808231d41](https://unsplash.com/photos/1590650153855-d9e808231d41) | Property manager standing in an office | tenant-relation, about |
| `two-people-laptop` | [1517245386807-bb43f82c33c4](https://unsplash.com/photos/1517245386807-bb43f82c33c4) | Two people discussing work at a laptop | tenant-relation, demo |
| `team-meeting` | [1521737604893-d14cc237f11d](https://unsplash.com/photos/1521737604893-d14cc237f11d) | Team meeting at a long table | how-it-works, demo |
| `boardroom` | [1542744173-8e7e53415bb0](https://unsplash.com/photos/1542744173-8e7e53415bb0) | Presentation in a boardroom | demo, resources |
| `checking-plans` | [1503387762-592deb58ef4e](https://unsplash.com/photos/1503387762-592deb58ef4e) | Supervisor reviewing a work log | work-orders |
| `site-workers` | [1541888946425-d81bb19240f5](https://unsplash.com/photos/1541888946425-d81bb19240f5) | Workers in safety vests on site | work-orders, mobile-staff |
| `devices-table` | [1519389950473-47ba0277781c](https://unsplash.com/photos/1519389950473-47ba0277781c) | Phones and laptops on a shared table | download, mobile-staff |
| `laptop-dashboard` | [1460925895917-afdab827c52f](https://unsplash.com/photos/1460925895917-afdab827c52f) | Laptop showing an analytics dashboard | home, platform |
| `planning-board` | [1552664730-d307ca884978](https://unsplash.com/photos/1552664730-d307ca884978) | Team planning with sticky notes | pricing |
| `office-workers` | [1504384308090-c894fdcc538d](https://unsplash.com/photos/1504384308090-c894fdcc538d) | People working in a busy office | resources |
| `phone-laptop` | [1559526324-4b87b5e36e44](https://unsplash.com/photos/1559526324-4b87b5e36e44) | Person using a laptop and phone | tenant-app, resources |

To add an image: append it to `images.json`, run `npm run images`, then reference it with `<Picture image="key" />`.
