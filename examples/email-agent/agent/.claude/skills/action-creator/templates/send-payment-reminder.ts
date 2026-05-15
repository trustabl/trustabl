// agent/custom_scripts/actions/send-payment-reminder-to-vendor.ts
// Template for sending payment reminders to a specific vendor

import type { ActionTemplate, ActionContext, ActionResult } from "../types";

export const config: ActionTemplate = {
  id: "send_payment_reminder_vendor",
  name: "Send Payment Reminder to Vendor",
  description: "Send a payment reminder email to a specific vendor for overdue invoices",
  icon: "💰",
  parameterSchema: {
    type: "object",
    properties: {
      vendorEmail: {
        type: "string",
        description: "Vendor's accounts payable email address"
      },
      vendorName: {
        type: "string",
        description: "Vendor company name"
      },
      invoiceNumber: {
        type: "string",
        description: "Invoice number (e.g., INV-2024-001)"
      },
      amount: {
        type: "string",
        description: "Invoice amount (e.g., '$5,000')"
      },
      dueDate: {
        type: "string",
        description: "Original due date"
      },
      daysPastDue: {
        type: "number",
        description: "Number of days past due"
      }
    },
    required: ["vendorEmail", "vendorName", "invoiceNumber", "amount", "dueDate", "daysPastDue"]
  }
};

export async function handler(
  params: Record<string, any>,
  context: ActionContext
): Promise<ActionResult> {
  const { vendorEmail, vendorName, invoiceNumber, amount, dueDate, daysPastDue } = params;

  context.log(`Sending payment reminder for ${invoiceNumber} to ${vendorName}`);

  const body = `Hi ${vendorName} Accounts Payable Team,

This is a friendly reminder that Invoice ${invoiceNumber} for ${amount} was due on ${dueDate} and is now ${daysPastDue} days past due.

Please process payment at your earliest convenience. If you have already sent payment, please disregard this notice.

Invoice Details:
- Invoice #: ${invoiceNumber}
- Amount: ${amount}
- Due Date: ${dueDate}
- Days Past Due: ${daysPastDue}

If you have any questions or need a copy of the invoice, please let me know.

Best regards`;

  try {
    const result = await context.sendEmail({
      to: vendorEmail,
      subject: `Payment Reminder: Invoice ${invoiceNumber} - ${daysPastDue} Days Past Due`,
      body
    });

    context.notify(`Payment reminder sent to ${vendorName} for ${invoiceNumber}`, {
      type: "success",
      priority: "normal"
    });

    return {
      success: true,
      message: `Payment reminder sent to ${vendorName} for ${invoiceNumber}`,
      data: { messageId: result.messageId, invoiceNumber, vendorName }
    };
  } catch (error: any) {
    context.log(`Failed to send payment reminder: ${error}`, "error");

    return {
      success: false,
      message: `Failed to send payment reminder: ${error.message}`
    };
  }
}
