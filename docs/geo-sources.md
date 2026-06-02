# IP geolocation data sources

A survey of free / open data sources for the `GeoLookup` service
(`geoip/`), evaluated for adding to the existing source set. There is no
single authoritative source for IP geolocation, so the strategy is to combine
several complementary sources and merge them (see `geoip/merge.go` and
[impl-notes.md](impl-notes.md#geolookup-service-ip-geolocation)).

## Evaluation criteria

| Criterion | Why it matters |
|---|---|
| **Granularity** | Country / region / city / **coordinates**. Our goal is lat/lon when available. |
| **Coverage** | Whole address space vs. a sparse high-value subset (e.g. infrastructure only). |
| **Authority** | Operator self-published (geofeeds) vs. measured (IPMap) vs. aggregated estimate (DB-IP/GeoLite2). |
| **License** | CC0/PDDL (free) ⟶ CC-BY (attribution) ⟶ CC-BY-SA (share-alike, viral) ⟶ account-gated. |
| **Format / access** | MMDB, CSV, or per-query API; download size; signup. |
| **Cadence** | Daily / twice-weekly / monthly. |
| **Fit** | How cleanly it drops into the `geoip.Source` interface. |

## Currently integrated

| Source | Granularity | Authority | License | Notes |
|---|---|---|---|---|
| **DB-IP City Lite** | country/region/city/postal **+ lat/lon** | aggregated estimate | CC-BY 4.0 | MMDB, monthly. `geoip/dbip.go`. |
| **RFC 8805 geofeeds** | country/region/city/postal (no coords) | operator self-published | per-publisher | discovered via RDAP + RPSL whois. `geoip/geofeed.go`. |

## Candidate sources

### RIPE IPmap — `https://ftp.ripe.net/ripe/ipmap/geolocations-latest`

**Strong candidate. Complements DB-IP rather than overlapping it.**

RIPE measures the location of **core Internet infrastructure** (routers, IXP
members, transit) using active measurement from the RIPE Atlas probe network
(latency / single-radius / crowdsourced / reverse-dns / anycast / ixp engines).
This is *measured* data, not registry- or estimate-derived, so for the IPs it
covers it is often more trustworthy than an aggregated DB.

- **Format** — bzip2'd CSV, one row per **exact** address (`/32` or `/128`):

  ```
  prefix,geolocation_id,city,state,country_name,cc_alpha2,cc_alpha3,latitude,longitude,score
  1.1.1.1/32,JOHANNESBURG-ZA-06-…,Johannesburg,Gauteng,South Africa,ZA,ZAF,-26.20227,28.04363,32.4
  2001:1900::3:32f/128,BOSTON-US-MA-…,Boston,Massachusetts,United States,US,USA,42.35843,-71.05977,29.64
  ```
- **Coverage** — ~602k exact IPs (≈70 MB uncompressed / 5 MB bz2), **no prefix
  ranges**. It only answers when the *exact queried IP* is a known node. Sparse
  for arbitrary end-user IPs; excellent for infrastructure/router IPs (where it
  often disagrees with — and beats — estimate DBs).
- **`score`** — a **relative sort factor, NOT an accuracy percentage** (per RIPE
  docs). Observed range roughly −26 … +46. Use only to rank IPmap's own
  candidates, not to compare against other sources.
- **License** — **RIPE NCC Terms of Service** (not a Creative Commons license).
  Openly downloadable; verify the ToS before redistribution. Updated **daily**;
  `geolocations-latest` is the newest dump.
- **Fit** — load the CSV into an exact-match map keyed by `netip.Addr`
  (`geoip/ipmap.go`), download in `setup.sh` alongside DB-IP. Granularity
  `COORDINATES`; `authoritative=false` but high practical trust for infra.

### IP2Location LITE (DB5 / DB11) — **INTEGRATED (opt-in)**

- **Granularity** — country/region/city **+ lat/lon** (DB5); DB11 adds postal +
  IANA timezone. Full-range coverage (integer ranges), comparable to DB-IP.
- **License** — **CC-BY-SA 4.0** (share-alike). Imposes no extra burden here
  because we do not redistribute the DB or a derived database (see
  [Licensing](#licensing-considerations)); attribution is still required.
- **Format** — implemented from the **DB9 MMDB**. IP2Location ships the LITE
  MMDB in the **GeoIP2-City schema** (metadata: `DatabaseType GeoLite2-City`),
  so it reuses our existing `maxminddb-golang/v2` reader via the shared
  `MMDBCitySource` — **mmap'd (negligible memory), one file for v4+v6**. MMDB is
  the open MaxMind format, not the proprietary `.BIN`. (An earlier CSV
  implementation was dropped: it held ~8.7M ranges at ~2 GB RAM, whereas the
  MMDB build adds ~0 and also carries region/postal/timezone the DB5 CSV lacked.)
- **Status** — shipped as an opt-in source: `setup.sh` downloads `DB9LITEMMDB`
  only when `IP2LOCATION_TOKEN` is set; geo-server loads it if present. Second
  whole-space estimate for cross-checking/filling DB-IP gaps. Full server with
  all four sources measures ≈ 215 MB RSS.

### MaxMind GeoLite2 City

- **Granularity** — country/region/city **+ lat/lon + timezone + accuracy
  radius**. The most recognised free city DB.
- **License** — **CC-BY-SA 4.0**, and the official download is **account-gated**
  (free MaxMind account + license key, redistribution restricted). A
  no-account CC-BY-SA mirror exists via `sapics/ip-location-db` (below).
- **Format** — MMDB (drop-in for our existing `maxminddb-golang/v2` reader) or
  CSV. Updated twice weekly.
- **Fit** — trivial to add (same reader as DB-IP, different field schema —
  GeoLite2 populates `subdivisions`/`postal`/`location.accuracy_radius` more
  fully). Gated behind the account + share-alike considerations.

### `sapics/ip-location-db` (aggregator / CDN)

GitHub project that **repackages** many DBs (DB-IP Lite, GeoLite2, NRO
geofeed+whois+ASN, RouteViews, IPtoASN) into uniform CSV + MMDB, served over
jsDelivr/unpkg with **no signup**. Useful as:
- a **CDN mirror** for the DB-IP City data we already fetch, and
- a way to get **GeoLite2 City** (CC-BY-SA) **without a MaxMind account**, and
- ready-made **ASN** and **country** datasets.

Licenses are per-dataset (CC-BY 4.0 for DB-IP/NRO, CC-BY-SA for GeoLite2,
PDDL for IPtoASN). Not a new data source per se, but the easiest distribution
channel for several of the above.

### Country / ASN-only sources (no coordinates)

Useful as a **country-level floor** and for ASN enrichment, but they cannot
satisfy the lat/lon goal on their own:

| Source | Content | License | Notes |
|---|---|---|---|
| **IPLocate free** | country + ASN | CC-BY-SA 4.0 | CSV + MMDB, daily, no signup. |
| **IPinfo Lite** | country + ASN | free tier (attribution) | As of 2025 the free tier is **country-level only**. |
| **IPtoASN** (`iptoasn.com`) | ASN + country | **PDDL 1.0** (public domain) | **INTEGRATED** (`geoip/iptoasn.go`) — origin-ASN enrichment + country floor + confidence. RouteViews/RIS-derived. |
| **RIR delegated-extended stats** | country per allocation | RIR terms (open) | Authoritative country from the five RIRs' daily `delegated-*-extended-latest`. Good cross-check against geofeed/DB country. |

## Licensing considerations

The repo currently ships **CC-BY 4.0** sources (DB-IP) which only require
attribution (already in `README.md`). Two cliffs to watch when adding more:

- **CC-BY-SA 4.0 (share-alike)** — IP2Location LITE, GeoLite2, IPLocate. The
  share-alike term triggers only when you **Share** (distribute to the public)
  **Adapted Material** — i.e. the database or a derived *database* (CC 4.0
  §4(b)); answering individual per-IP queries at runtime is normal use, not
  Sharing. Because our download-at-setup pattern never commits or redistributes
  the data, **SA imposes no extra burden over CC-BY** for us — attribution
  (required by both) is the only live obligation, and it rides in each
  `GeoSourceResult`. **Invariant to preserve this:** do **not** add a bulk-dump
  / "export the merged database" endpoint sourced from SA data — that would be
  Sharing a derivative and would relicense the output under BY-SA. Keep
  share-alike sources **opt-in** and never commit their files (they are
  gitignored).
- **Account-gated (GeoLite2 official)** — needs a license key; avoid making it a
  default. The `sapics` CC-BY-SA mirror sidesteps the account but not the
  share-alike terms.
- **RIPE NCC ToS / RIR terms** — not Creative Commons; openly usable but read
  the ToS before redistributing. IPMap and the delegated stats fall here.

`geoip/dbip.go`'s `DBIPAttribution` constant pattern should be extended: every
source carries its own `attribution` string in `GeoSourceResult`, which the
client already prints — keep that honest for each new source.

## Recommendations (priority order)

1. **RIPE IPmap** — highest marginal value: *measured*, coordinate-bearing, and
   complementary (infra IPs where estimate DBs are weakest). Permissive enough
   (RIPE ToS), modest size, daily. Add as `geoip/ipmap.go` with a setup-time
   download. **Top pick.**
2. ~~**A second whole-space coordinate DB.**~~ **Done** — IP2Location LITE DB9
   shipped as an opt-in MMDB source (`geoip/ip2location.go` + shared
   `geoip/mmdb.go`). GeoLite2 City remains a future option (best accuracy, but
   adds MaxMind's EULA on top of CC-BY-SA — see above); note its MMDB would slot
   straight into the same `MMDBCitySource`.
3. **RIR delegated-extended stats** — cheap, authoritative **country floor** that
   fills `GeoResponse.best.country` when nothing finer is available.
4. ~~**IPtoASN (PDDL)** — ASN enrichment with zero license friction.~~ **Done**
   (`geoip/iptoasn.go`): origin ASN + country floor.

### BGP-derived signals (added)

Public BGP data carries no coordinates but supplies enrichment + quality:
- **iptoasn** (above) → origin ASN + network name on `GeoResponse.asn/network`.
- **bgp.tools anycast prefixes** (`geoip/anycast.go`) → `GeoResponse.anycast`,
  which forces `confidence = LOW`. This is the key quality fix for anycast/CDN
  IPs where a single coordinate is meaningless and the estimate sources
  disagree (1.1.1.1, 8.8.8.8). A `GeoConfidence` axis (HIGH measured/authoritative,
  MEDIUM estimate, LOW floor/anycast) now rides on every result — implementing
  the confidence follow-up noted earlier.
RouteViews/RIPE RIS MRT and CAIDA pfx2as remain alternatives if we ever want
to build the IP→ASN table ourselves instead of consuming iptoasn.

### Suggested merge/proto follow-ups

- ~~Add an optional **`confidence`** concept to `GeoSourceResult`.~~ **Done** —
  `GeoConfidence` (LOW/MEDIUM/HIGH) on both `GeoSourceResult` and `GeoResponse`,
  with anycast forcing LOW. (IPmap's `score` is relative-only and still unused;
  fold it in per-source if finer weighting is wanted later.)
- ~~Generalise the setup-time download into a small manifest of
  `{url, dest, cadence}` so new file-based sources are one table entry.~~
  **Done** — see the `GEO_SOURCES` manifest + `fetch_geo_source` in `setup.sh`
  (rows are `name|url|dest|freshness|postprocess`, with `{YYYY-MM}` templating,
  `monthly`/`Nd` freshness, and `gunzip`/`none` postprocessing).

## Sources

- [RIPE IPmap](https://ipmap.ripe.net/) · [manual](https://ipmap.ripe.net/docs/01.manual/) · [FTP dumps](https://ftp.ripe.net/ripe/ipmap/) · [RIPE NCC ToS](https://www.ripe.net/about-us/legal/terms-of-service)
- [IP2Location LITE](https://lite.ip2location.com/) · [DB5 (lat/lon)](https://lite.ip2location.com/database/db5-ip-country-region-city-latitude-longitude)
- [MaxMind GeoLite2](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data/) · [signup](https://www.maxmind.com/en/geolite2/signup)
- [sapics/ip-location-db](https://github.com/sapics/ip-location-db)
- [IPLocate free databases](https://www.iplocate.io/blog/meet-iplocate-free-ip-to-country-ip-to-asn-databases)
- [IPinfo Lite](https://ipinfo.io/lite) · [IPtoASN](https://iptoasn.com/)
