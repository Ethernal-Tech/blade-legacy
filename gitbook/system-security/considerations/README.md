# Considerations

This section is dedicated to outlining a comprehensive strategy for ensuring data security within the Blade chain for multiple cloud providers. The primary objective is to implement measures facilitating the smooth deployment of Blade across different vendors, thereby mitigating the risk of vendor lock-in. The chapter aims to articulate the necessary steps for adopting a cloud-first Secure by Design approach.

As the focus is on **confidentiality**, the discussion delves into three key aspects of encryption within Blade data protection framework:

1. Data at rest encryption
2. Data in transit encryption
3. Data in use encryption

It is essential to discern the primary focus of each encryption aspect across different scenarios. To achieve this, a **risk-based methodology** is presented, along with tailored response strategies for identified threats.

To elaborate further, the chapter addresses the following specific areas:

* Encryption protocols relevant to Infrastructure as a Service (IaaS) and general compute environments (e.g., Azure Virtual Machine, AWS EC2, and GCP Compute Engine), including virtual disks.
* Implementation of the gRPC framework to ensure secure communication, with a particular emphasis on encryption in transit.
* Encryption measures for RPC via HTTPS requests originating from the computational environment.
* Secure management of keys that involves implementing robust protocols, strict access controls, and secure storage solutions such as Key Vaults or Hardware Security Modules (HSMs).

## Preconditions and Inputs

To ensure data protection of Blade, it is necessary to execute the following steps:

1. Data classification / Inventory
2. Regulations, requirements, and industry best practices
3. Design reviews and threat modeling
4. Establishing Data Protection processes

**Data classification** tags data according to its type, sensitivity, and value to the organization if altered, stolen, or destroyed. It helps an organization understand the value of its data, determine whether the data is at risk, and implement controls to mitigate risks. As a first step, it’s necessary to define different data categories and describe them. Some sensitive data like keys, secrets, and other authentication identifiers are clearly sensitive data, but when it comes to business-critical data and PII (personally identifiable information), the data classification needs special focus and attention.

**Requirements** are sourced from applicable laws, executive orders, directives, regulations, policies, standards, procedures, or mission/business needs to ensure the confidentiality, integrity, and availability of information that is being processed, stored, or transmitted. ([definition](http://csrc.nist.gov/glossary/term/security\_requirement)). However, it is of utmost importance to perform proper **threat modeling** to elicit additional requirements in the context of the software design that will lead to proper design reviews ensuring Secure by Design is a driving principle from initial stages of software development.

**Data protection processes** are methods and practices to safeguard personal data from unauthorized or unlawful access, use, disclosure, alteration, or destruction. The processes need to be established to ensure software vendor’s alignment with regulations and laws.
