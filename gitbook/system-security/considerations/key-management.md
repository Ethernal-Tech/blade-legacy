# Key Management

## Cryptography <a href="#heading-h.dtb7kuyiv1zv" id="heading-h.dtb7kuyiv1zv"></a>

FIPS 140-2 specifies the security requirements for cryptographic modules and is **a recommended resource** if no other specific requirement exists. These requirements are essential for maintaining the confidentiality and integrity of sensitive data.

FIPS 140-2 defines four increasing levels of assurance for cryptographic modules. These levels cover a wide range of potential applications and environments (see [here](https://csrc.nist.gov/pubs/fips/140-2/upd2/final)):

* Level 1: The simplest requirements. It mandates production-grade equipment and at least one tested encryption algorithm. The algorithm must be authorized for use.
* Level 2: Adds tamper-evident physical security features to Level 1 requirements.
* Level 3: Enhances physical security to resist tampering attempts.
* Level 4: Provides robust protection against physical attacks.

All cloud vendors support up to Level 3 compliance on HSM (Hardware Security Module) devices.

Although FIPS 140-2 is often cited regarding cryptographic requirements, it is recommended to prioritize up-to-date libraries available in different code languages rather than insisting on FIPS 140-2 compliant libraries. Some open-source and closed source libraries are industry proven and do not even forego official attestation by FIPS which makes FIPS compliant libraries often older and therefore less secure due to the slow process of certifying them. If it is required by a regulation, then design should account for FIPS 140-2 approved libraries only. Learn [here](https://techcommunity.microsoft.com/t5/microsoft-security-baselines/why-we-re-not-recommending-fips-mode-anymore/ba-p/701037) why Microsoft is not recommending FIPS compliant encryption libraries anymore.

**The recommendations:**

1. For key management practices, refer to Table 1 of NIST SP 800-57 (see [here](https://csrc.nist.gov/pubs/sp/800/57/pt1/r5/final)).
2. Symmetric key size and algorithm: AES256 is a default for encryption at rest by cloud vendors and still considered good enough for protecting data at rest.
3. Asymmetric key sizes and algorithm: both RSA and ECC are widely accepted across industries, and it is recommended to use RSA with key size of 4096 or ECC P-384 assuming longer rotation periods are expected and intentionally descoping the issue of quantum-safe cryptography. If cryptographic keys need to be shorter due to performance issues, then rotation practices would need to be reevaluated. Generating the right key would also depend on the PKI used.

## Key Vaults <a href="#heading-h.31gc7c6cpqnh" id="heading-h.31gc7c6cpqnh"></a>

The key management services offered by Azure, AWS and GCP are Azure Key Vault, AWS Key Management Service (KMS) and GCP KMS, respectively. All three are used for managing cryptographic keys, secrets with support for automatic key rotation and logging for accountability.

The most important thing to evaluate is the need for compliance – typically this means aligning with FIPS 140-2, level 2 or level 3 managed HSM devices which would depend on the industry for which the application is being developed. As a rule of thumb, one should go with level 3 if it is in the sensitive software industry. In any case, all providers can support the same levels of compliance.

**Blade key management:** Secure the Blade node private keys by leveraging Key Management Services (KMS) for secure storage and management. Additionally, KMS solutions offer hardware security modules (HSMs) that provide cryptographic storage, ensuring keys are safeguarded against unauthorized access.

Note: In order to apply an improved security framework Blade team can implement the support for the customer’s choice to bring their own keys or use CSP service to generate one. This decision typically is a balance of existing customer’s capabilities, regulation concerns and trust relationship with CSPs as it is all about having complete control with a price of additional management complexity. The benefits of bringing your own key include more granular control over key creation, rotation, and it provides segregation from cloud infrastructure and cloud processes around key management. Unless required by data security regulations, the recommendation is to assign a lower priority within one’s system security program on building and maintaining an infrastructure for generating and maintaining the keys.
