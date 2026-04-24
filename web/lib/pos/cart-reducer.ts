import Decimal from "decimal.js"
import type { MeasurementStrategy } from "@/lib/api/products"

export interface CartItem {
  productId: string
  productName: string
  unitId: string
  unitName: string
  unitPrice: Decimal
  quantity: Decimal
  measurementStrategy: MeasurementStrategy
}

export type DiscountType = "percent" | "fixed"

export interface CartState {
  items: CartItem[]
  discount: Decimal
  discountType: DiscountType
  remark: string
}

export type CartAction =
  | { type: "ADD_ITEM"; item: CartItem }
  | { type: "REMOVE_ITEM"; productId: string }
  | { type: "SET_QUANTITY"; productId: string; quantity: Decimal }
  | { type: "SET_UNIT_PRICE"; productId: string; unitPrice: Decimal }
  | { type: "APPLY_DISCOUNT"; amount: Decimal; discountType: DiscountType }
  | { type: "SET_REMARK"; remark: string }
  | { type: "CLEAR_CART" }

export const initialCartState: CartState = {
  items: [],
  discount: new Decimal(0),
  discountType: "fixed",
  remark: "",
}

/**
 * Compute the subtotal of all cart items using Decimal.js to avoid floating-point errors.
 * Does NOT apply discount — call cartNetTotal for the final amount.
 */
export function cartTotal(items: CartItem[]): Decimal {
  return items.reduce(
    (acc, item) => acc.plus(item.unitPrice.times(item.quantity)),
    new Decimal(0)
  )
}

/**
 * Compute the net total after applying discount.
 */
export function cartNetTotal(state: CartState): Decimal {
  const subtotal = cartTotal(state.items)
  if (state.discountType === "fixed") {
    return Decimal.max(subtotal.minus(state.discount), new Decimal(0))
  }
  // percent: discount is 0-100
  const factor = new Decimal(100).minus(state.discount).dividedBy(100)
  return subtotal.times(factor)
}

export function cartReducer(state: CartState, action: CartAction): CartState {
  switch (action.type) {
    case "ADD_ITEM": {
      const existing = state.items.find((i) => i.productId === action.item.productId)
      if (existing) {
        return {
          ...state,
          items: state.items.map((i) =>
            i.productId === action.item.productId
              ? { ...i, quantity: i.quantity.plus(action.item.quantity) }
              : i
          ),
        }
      }
      return { ...state, items: [...state.items, action.item] }
    }

    case "REMOVE_ITEM": {
      return {
        ...state,
        items: state.items.filter((i) => i.productId !== action.productId),
      }
    }

    case "SET_QUANTITY": {
      if (action.quantity.lte(0)) {
        return {
          ...state,
          items: state.items.filter((i) => i.productId !== action.productId),
        }
      }
      return {
        ...state,
        items: state.items.map((i) =>
          i.productId === action.productId ? { ...i, quantity: action.quantity } : i
        ),
      }
    }

    case "SET_UNIT_PRICE": {
      return {
        ...state,
        items: state.items.map((i) =>
          i.productId === action.productId ? { ...i, unitPrice: action.unitPrice } : i
        ),
      }
    }

    case "APPLY_DISCOUNT": {
      return { ...state, discount: action.amount, discountType: action.discountType }
    }

    case "SET_REMARK": {
      return { ...state, remark: action.remark }
    }

    case "CLEAR_CART": {
      return { ...initialCartState, discount: new Decimal(0) }
    }

    default:
      return state
  }
}
