#!/usr/bin/env bash
set -euo pipefail

# Remove both trust paths described at https://emudhra.we-have-a-problem.com/.
sed -i \
  -e 's|^mozilla/Comodo_AAA_Services_root.crt$|!mozilla/Comodo_AAA_Services_root.crt|' \
  -e 's|^mozilla/emSign_ECC_Root_CA_-_G3.crt$|!mozilla/emSign_ECC_Root_CA_-_G3.crt|' \
  /etc/ca-certificates.conf
update-ca-certificates --fresh
