import { rpc } from "./client";

const SVC = "CertificateService";

export interface CertificateItem {
  id: string;
  domain: string;
  bucket: string;
  region: string;
  accountName: string;
  status: "pending" | "active" | "error";
  issuedAt: string;
  expiresAt: string;
  errorMessage: string;
}

export interface ListCertificatesResponse {
  certificates: CertificateItem[];
}

export function listCertificates() {
  return rpc<object, ListCertificatesResponse>(SVC, "ListCertificates", {});
}
