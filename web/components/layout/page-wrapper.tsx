"use client";

import { usePathname } from "next/navigation";
import { motion } from "framer-motion";

export function PageWrapper({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  return (
    <motion.div
      key={pathname}
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: "easeOut" }}
      className="px-6 py-6"
    >
      {children}
    </motion.div>
  );
}
