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

### IP2Location LITE (DB5 / DB11)

- **Granularity** — country/region/city **+ lat/lon** (DB5); DB11 adds postal +
  IANA timezone. Full-range coverage (prefix ranges), comparable to DB-IP.
- **License** — **CC-BY-SA 4.0** (share-alike). Attribution **and** derivatives
  must carry the same license — see [Licensing](#licensing-considerations).
- **Format** — proprietary `.BIN` (needs the IP2Location Go reader) or CSV.
  Monthly; free signup/token for direct download.
- **Fit** — second whole-space coordinate DB; good for cross-checking DB-IP and
  filling its gaps (its region/postal coverage is often better). The share-alike
  license is the main reason it was deferred from v1.

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
| **IPtoASN** (`iptoasn.com`) | ASN + country | **PDDL 1.0** (public domain) | The only truly attribution-free option. |
| **RIR delegated-extended stats** | country per allocation | RIR terms (open) | Authoritative country from the five RIRs' daily `delegated-*-extended-latest`. Good cross-check against geofeed/DB country. |

## Licensing considerations

The repo currently ships **CC-BY 4.0** sources (DB-IP) which only require
attribution (already in `README.md`). Two cliffs to watch when adding more:

- **CC-BY-SA 4.0 (share-alike)** — IP2Location LITE, GeoLite2, IPLocate. If we
  redistribute these databases or a derivative *of the database*, the derivative
  inherits CC-BY-SA. Querying them at runtime to produce a `GeoResponse` is
  normal use; bundling/redistributing the data files is the trigger. Keep
  share-alike sources **optional and clearly labelled**, and prefer not to
  commit their data into the repo.
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
2. **A second whole-space coordinate DB** — either GeoLite2 City (best accuracy,
   but CC-BY-SA + account/mirror) or IP2Location LITE (CC-BY-SA, good
   region/postal). Adding one lets the merge cross-check/agree on coordinates and
   raises city-level coverage. Ship it **opt-in** because of share-alike.
3. **RIR delegated-extended stats** — cheap, authoritative **country floor** that
   fills `GeoResponse.best.country` when nothing finer is available.
4. **IPtoASN (PDDL)** — if we want ASN enrichment with zero license friction.

### Suggested merge/proto follow-ups

- Add an optional **`confidence`/`score`** concept to `GeoSourceResult` so the
  merge can weight measured (IPmap) and authoritative (geofeed) results above
  bulk estimates, instead of relying solely on `granularity` + `authoritative`.
  (IPmap's `score` is relative-only, so normalise per source.)
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
