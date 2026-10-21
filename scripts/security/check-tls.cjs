"use strict";

const https = require("node:https");
const hosts = [
  "dv-g3b-emudhra.we-have-a-problem.com",
  "dv-g3b-aaa-emudhra.we-have-a-problem.com",
  "example.com",
];
const certificateErrors = new Set([
  "CERT_REVOKED",
  "UNABLE_TO_GET_ISSUER_CERT_LOCALLY",
  "UNABLE_TO_VERIFY_LEAF_SIGNATURE",
  "CERT_UNTRUSTED",
]);

function get(url) {
  return new Promise((resolve, reject) => {
    const request = https.get(
      url,
      { signal: AbortSignal.timeout(20000) },
      (response) => {
        response.resume();
        resolve(response.statusCode);
      },
    );
    request.on("error", reject);
  });
}

async function main() {
  const transport = "node:https";
  const request = get;
  for (const host of hosts) {
    for (let attempt = 1; attempt <= 3; attempt++) {
      let status;
      let error;
      try {
        status = await request(`https://${host}`);
      } catch (caught) {
        error = caught.cause?.code || caught.code || caught.name;
      }
      const pass =
        host === "example.com"
          ? status === 200 && !error
          : certificateErrors.has(error);
      console.log(
        JSON.stringify({ transport, host, attempt, status, error, pass }),
      );
      if (pass) break;
      if (!error || certificateErrors.has(error) || attempt === 3) {
        process.exitCode = 1;
        break;
      }
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
