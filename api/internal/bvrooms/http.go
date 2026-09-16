package bvrooms

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
	DB  *db.DB
}

// withOrg: endpoint publik — organization dari query organization_slug / header X-Org-Slug (pola Tenant PWA).
func (h *Handler) withOrg(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.URL.Query().Get("organization_slug")
		if slug == "" {
			slug = r.Header.Get("X-Org-Slug")
		}
		id, _, err := h.Svc.ResolveOrg(r.Context(), slug)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithOrgID(r.Context(), id)))
	})
}

// optionalCustomer: token customer bila ada (is_wishlisted di katalog), tanpa menolak request tanpa token.
func (h *Handler) optionalCustomer(next http.Handler) http.Handler {
	auth := h.Svc.Authenticate(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			auth.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func cacheable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		next.ServeHTTP(w, r)
	})
}

// MountPublic: /bvrooms/app-config, /bvrooms/auth/*, /bvrooms/catalog/* (tanpa token; org via slug).
func (h *Handler) MountPublic(r chi.Router) {
	r.Route("/bvrooms", func(r chi.Router) {
		r.Get("/app-config", h.appConfig)
		r.Post("/auth/otp/request", h.otpRequest)
		r.Post("/auth/otp/verify", h.otpVerify)
		r.Post("/auth/register", h.register)
		r.Post("/auth/refresh", h.refresh)
		r.Group(func(r chi.Router) {
			r.Use(h.withOrg, h.optionalCustomer, cacheable)
			r.Get("/catalog/properties", h.listProperties)
			r.Get("/catalog/banners", h.banners)
			r.Get("/catalog/properties/{slug}", h.property)
			r.Get("/catalog/properties/{slug}/photos", h.photos)
			r.Get("/catalog/properties/{slug}/room-types", h.roomTypes)
			r.Get("/catalog/properties/{slug}/room-types/{typeId}/calendar", h.calendar)
			r.Get("/catalog/properties/{slug}/reviews/summary", h.reviewSummary)
		})
		// endpoint customer (token bvrooms_customer)
		r.Group(func(r chi.Router) {
			r.Use(h.Svc.Authenticate)
			if h.DB != nil {
				r.Use(httpx.Idempotency(h.DB))
			}
			r.Post("/auth/logout", h.logout)
			r.Get("/customers/me", h.me)
			r.Patch("/customers/me", h.updateMe)
			r.Get("/customers/me/wishlist", h.wishlist)
			r.Post("/customers/me/wishlist", h.addWishlist)
			r.Delete("/customers/me/wishlist/{propertyId}", h.removeWishlist)
			r.Get("/customers/me/notifications", h.notifications)
			r.Post("/customers/me/notifications/read-all", h.readAll)
			r.Post("/customers/me/notifications/{id}/read", h.readOne)
			r.Delete("/customers/me/notifications/{id}", h.deleteNotification)
			r.Post("/customers/me/push-subscriptions", h.subscribePush)
			r.Delete("/customers/me/push-subscriptions", h.unsubscribePush)
			r.Get("/payment-methods", h.paymentMethods)
			r.Get("/bookings", h.listBookings)
			r.Post("/bookings", h.createBooking)
			r.Get("/bookings/{code}", h.getBooking)
			r.Patch("/bookings/{code}/guest", h.modifyGuest)
			r.Post("/bookings/{code}/cancel", h.cancelBooking)
			r.Post("/bookings/{code}/review", h.review)
			r.Post("/bookings/{code}/payment", h.createPayment)
			r.Post("/bookings/{code}/payment/change-method", h.createPayment)
			r.Post("/bookings/{code}/payment/proof/presign", h.proofPresign)
			r.Post("/bookings/{code}/payment/proof", h.proofConfirm)
		})
	})
}

// MountAdmin: dashboard staf (IAM token) — /bvrooms/admin/*.
func (h *Handler) MountAdmin(r chi.Router) {
	req := h.IAM.Require
	r.Route("/bvrooms/admin", func(r chi.Router) {
		r.With(req("bvrooms.listing.view")).Get("/properties/{id}/listing", h.getListing)
		r.With(req("bvrooms.listing.manage")).Put("/properties/{id}/listing", h.updateListing)
		r.With(req("bvrooms.listing.manage")).Post("/properties/{id}/photos/presign", h.photoPresign)
		r.With(req("bvrooms.listing.manage")).Post("/properties/{id}/photos/{photoId}/confirm", h.photoConfirm)
		r.With(req("bvrooms.listing.manage")).Patch("/properties/{id}/photos/{photoId}", h.photoPatch)
		r.With(req("bvrooms.listing.manage")).Delete("/properties/{id}/photos/{photoId}", h.photoDelete)
		r.With(req("bvrooms.addons.view")).Get("/properties/{id}/addons", h.listAddons)
		r.With(req("bvrooms.addons.manage")).Post("/properties/{id}/addons", h.createAddon)
		r.With(req("bvrooms.addons.manage")).Patch("/properties/{id}/addons/{addonId}", h.updateAddon)
		r.With(req("bvrooms.inventory.view")).Get("/unit-types", h.listUnitTypes)
		r.With(req("bvrooms.inventory.manage")).Post("/unit-types", h.createUnitType)
		r.With(req("bvrooms.inventory.manage")).Patch("/unit-types/{id}", h.updateUnitType)
		r.With(req("bvrooms.inventory.manage")).Patch("/units/{id}", h.setUnitRental)
		r.With(req("bvrooms.marketing.view")).Get("/promotions", h.listPromotions)
		r.With(req("bvrooms.marketing.manage")).Post("/promotions", h.createPromotion)
		r.With(req("bvrooms.marketing.manage")).Patch("/promotions/{id}", h.updatePromotion)
		r.With(req("bvrooms.marketing.view")).Get("/banners", h.listBanners)
		r.With(req("bvrooms.marketing.manage")).Post("/banners", h.createBanner)
		r.With(req("bvrooms.marketing.manage")).Patch("/banners/{id}", h.updateBanner)
		r.With(req("bvrooms.bookings.view")).Get("/bookings", h.adminListBookings)
		r.With(req("bvrooms.bookings.view")).Get("/bookings/{id}", h.adminGetBooking)
		for _, a := range []string{"confirm", "check_in", "check_out", "cancel", "no_show"} {
			r.With(req("bvrooms.bookings.manage")).Post("/bookings/{id}/"+a, h.adminAction(a))
		}
		r.With(req("bvrooms.payments.verify")).Post("/bookings/{id}/refund-done", h.refundDone)
		r.With(req("bvrooms.payments.verify")).Post("/payments/{id}/verify", h.verifyPayment)
		r.With(req("bvrooms.payments.verify")).Post("/payments/{id}/reject", h.rejectPayment)
		r.With(req("bvrooms.customers.view")).Get("/customers", h.adminCustomers)
		r.With(req("bvrooms.customers.manage")).Post("/customers/{id}/block", h.setCustomerStatus("blocked"))
		r.With(req("bvrooms.customers.manage")).Post("/customers/{id}/unblock", h.setCustomerStatus("active"))
	})
}

// ---------- helper ----------

func writeErr(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, err) }

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	return httpx.PathUUID(r, chi.URLParam, key)
}

func queryDate(r *http.Request, key string) (*time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	t, err := parseDate(v)
	if err != nil {
		return nil, apperr.Validation(key + " harus YYYY-MM-DD")
	}
	return &t, nil
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func queryFloat(r *http.Request, key string) *float64 {
	if v := r.URL.Query().Get(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
}

// ---------- app config & auth ----------

func (h *Handler) appConfig(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.AppConfig(r.Context(), r.URL.Query().Get("organization_slug"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) otpRequest(w http.ResponseWriter, r *http.Request) {
	var in OTPRequestInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.RequestOTP(r.Context(), in, httpx.ClientIP(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) otpVerify(w http.ResponseWriter, r *http.Request) {
	var in OTPVerifyInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.VerifyOTP(r.Context(), in, httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var in RegisterInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.Register(r.Context(), in, httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OrganizationSlug string `json:"organization_slug"`
		RefreshToken     string `json:"refresh_token"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.Refresh(r.Context(), in.OrganizationSlug, in.RefreshToken, httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.Logout(r.Context()); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- katalog ----------

func (h *Handler) listProperties(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := CatalogFilter{Q: q.Get("q"), Category: q.Get("category"), Lat: queryFloat(r, "lat"), Lng: queryFloat(r, "lng"), Rooms: queryInt(r, "rooms", 1), Adults: queryInt(r, "adults", 1), Children: queryInt(r, "children", 0), Sort: q.Get("sort"), Cursor: q.Get("cursor"), Limit: queryInt(r, "limit", 25)}
	var err error
	if f.CheckIn, err = queryDate(r, "check_in"); err != nil {
		writeErr(w, r, err)
		return
	}
	if f.CheckOut, err = queryDate(r, "check_out"); err != nil {
		writeErr(w, r, err)
		return
	}
	items, next, err := h.Svc.ListProperties(r.Context(), f)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) banners(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	items, err := h.Svc.Banners(r.Context(), pid)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) property(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.GetProperty(r.Context(), chi.URLParam(r, "slug"), queryFloat(r, "lat"), queryFloat(r, "lng"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) photos(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Photos(r.Context(), chi.URLParam(r, "slug"), r.URL.Query().Get("category"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) roomTypes(w http.ResponseWriter, r *http.Request) {
	ci, err := queryDate(r, "check_in")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	co, err := queryDate(r, "check_out")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	items, err := h.Svc.RoomTypes(r.Context(), chi.URLParam(r, "slug"), ci, co, queryInt(r, "rooms", 1), queryInt(r, "adults", 1), queryInt(r, "children", 0))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	typeID, err := pathUUID(r, "typeId")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	items, err := h.Svc.Calendar(r.Context(), chi.URLParam(r, "slug"), typeID, month)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) reviewSummary(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.ReviewSummary(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ---------- customer ----------

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Me(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	var in UpdateMeInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.UpdateMe(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) wishlist(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.Wishlist(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) addWishlist(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PropertyID uuid.UUID `json:"property_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := h.Svc.AddWishlist(r.Context(), in.PropertyID); err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"property_id": in.PropertyID, "is_wishlisted": true})
}

func (h *Handler) removeWishlist(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "propertyId")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := h.Svc.RemoveWishlist(r.Context(), id); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) notifications(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.Notifications(r.Context(), page)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) readAll(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.MarkRead(r.Context(), nil); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) readOne(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := h.Svc.MarkRead(r.Context(), &id); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteNotification(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := h.Svc.DeleteNotification(r.Context(), id); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) subscribePush(w http.ResponseWriter, r *http.Request) {
	var in PushSubscriptionInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := h.Svc.SubscribePush(r.Context(), in); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unsubscribePush(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.UnsubscribePush(r.Context(), r.URL.Query().Get("endpoint")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- booking & pembayaran ----------

func (h *Handler) paymentMethods(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil || pid == nil {
		writeErr(w, r, apperr.Validation("property_id wajib"))
		return
	}
	items, err := h.Svc.PaymentMethods(r.Context(), *pid)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) listBookings(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	items, next, err := h.Svc.ListBookings(r.Context(), r.URL.Query().Get("scope"), page)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createBooking(w http.ResponseWriter, r *http.Request) {
	var in CreateBookingInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.CreateBooking(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) getBooking(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.GetBooking(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) modifyGuest(w http.ResponseWriter, r *http.Request) {
	var in GuestInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.ModifyGuest(r.Context(), chi.URLParam(r, "code"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) cancelBooking(w http.ResponseWriter, r *http.Request) {
	var in CancelInput
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &in); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	out, err := h.Svc.CancelBooking(r.Context(), chi.URLParam(r, "code"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	var in ReviewInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.CreateReview(r.Context(), chi.URLParam(r, "code"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var in CreatePaymentReq
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.CreatePayment(r.Context(), chi.URLParam(r, "code"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) proofPresign(w http.ResponseWriter, r *http.Request) {
	var in ProofPresignInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.ProofPresign(r.Context(), chi.URLParam(r, "code"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) proofConfirm(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.ProofConfirm(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ---------- admin ----------

func (h *Handler) getListing(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.GetListing(r.Context(), id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateListing(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in ListingInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.UpdateListing(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) photoPresign(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in PhotoPresignInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.PhotoPresign(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) photoConfirm(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	photoID, err := pathUUID(r, "photoId")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.PhotoConfirm(r.Context(), id, photoID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) photoPatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	photoID, err := pathUUID(r, "photoId")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in PhotoPatchInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.PhotoPatch(r.Context(), id, photoID, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) photoDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	photoID, err := pathUUID(r, "photoId")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := h.Svc.PhotoDelete(r.Context(), id, photoID); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listAddons(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	items, err := h.Svc.ListAddons(r.Context(), id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createAddon(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in AddonInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.CreateAddon(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateAddon(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	addonID, err := pathUUID(r, "addonId")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in AddonInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.UpdateAddon(r.Context(), id, addonID, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listUnitTypes(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil || pid == nil {
		writeErr(w, r, apperr.Validation("property_id wajib"))
		return
	}
	items, err := h.Svc.ListUnitTypes(r.Context(), *pid)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createUnitType(w http.ResponseWriter, r *http.Request) {
	var in UnitTypeInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.CreateUnitType(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateUnitType(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in UnitTypeInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.UpdateUnitType(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) setUnitRental(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in UnitRentalInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.SetUnitRental(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listPromotions(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListPromotions(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createPromotion(w http.ResponseWriter, r *http.Request) {
	var in PromotionInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.CreatePromotion(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updatePromotion(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in PromotionInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.UpdatePromotion(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listBanners(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListBanners(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createBanner(w http.ResponseWriter, r *http.Request) {
	var in BannerInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.CreateBanner(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateBanner(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in BannerInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.UpdateBanner(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) adminListBookings(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var f AdminBookingFilter
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		writeErr(w, r, err)
		return
	}
	f.Status = r.URL.Query().Get("status")
	f.PaymentStatus = r.URL.Query().Get("payment_status")
	f.Q = r.URL.Query().Get("q")
	if f.From, err = queryDate(r, "from"); err != nil {
		writeErr(w, r, err)
		return
	}
	if f.To, err = queryDate(r, "to"); err != nil {
		writeErr(w, r, err)
		return
	}
	items, next, err := h.Svc.AdminListBookings(r.Context(), f, page)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) adminGetBooking(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.AdminGetBooking(r.Context(), id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) adminAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeErr(w, r, err)
			return
		}
		var in AdminActionInput
		if r.ContentLength != 0 {
			if err := httpx.Decode(r, &in); err != nil {
				writeErr(w, r, err)
				return
			}
		}
		out, err := h.Svc.AdminBookingAction(r.Context(), id, action, in)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) verifyPayment(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in VerifyInput
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &in); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	out, err := h.Svc.VerifyPayment(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) rejectPayment(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in VerifyInput
	if err := httpx.Decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.Svc.RejectPayment(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) refundDone(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var in VerifyInput
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &in); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	out, err := h.Svc.RefundDone(r.Context(), id, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) adminCustomers(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	items, next, err := h.Svc.AdminListCustomers(r.Context(), r.URL.Query().Get("q"), page)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) setCustomerStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeErr(w, r, err)
			return
		}
		out, err := h.Svc.AdminSetCustomerStatus(r.Context(), id, status)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

var _ = authctx.From
