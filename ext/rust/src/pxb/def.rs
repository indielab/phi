//! The `pxb_message!` declarative macro.
//!
//! Generates a `#[derive(Debug, Clone, PartialEq, Eq, Default)]` struct with
//! `pub` fields, plus `encode(&self) -> Vec<u8>` and
//! `decode(b: &[u8]) -> Result<Self, Error>` methods, from a compact field
//! list. The `option` keyword marks a field as wire-optional: it is omitted
//! from the encoding when [`WireField::is_opt_zero`] returns `true`.

macro_rules! pxb_message {
    ($(#[$meta:meta])* pub struct $Name:ident { $($body:tt)* }) => {
        pxb_message!(@step $Name, [$(#[$meta])*], [], [], [], [ $($body)* ]);
    };

    (@step $Name:ident, [$(#[$meta:meta])*],
     [$($sf:tt)*], [$($eitem:tt)*], [$($ditem:tt)*], []) => {
        #[derive(Debug, Clone, PartialEq, Eq, Default)]
        $(#[$meta])*
        pub struct $Name { $($sf)* }
        impl $Name {
            pub fn encode(&self) -> Vec<u8> {
                let mut fw = FieldWriter::new();
                $( pxb_message!(@enc self, fw, $eitem); )*
                fw.into_vec()
            }
            pub fn decode(b: &[u8]) -> Result<Self, Error> {
                let mut m = Self::default();
                walk_fields(b, |tag, kind, fr| {
                    pxb_message!(@decode m, tag, kind, fr, $($ditem)*);
                    Ok(())
                })?;
                Ok(m)
            }
        }
    };

    (@step $Name:ident, [$(#[$meta:meta])*],
     [$($sf:tt)*], [$($eitem:tt)*], [$($ditem:tt)*],
     [$(#[$attr:meta])* $tag:literal => $field:ident : $ty:ty , $($rest:tt)*]) => {
        pxb_message!(@step $Name, [$(#[$meta])*],
            [$($sf)* $(#[$attr])* pub $field: $ty,],
            [$($eitem)* {+ $tag $field}],
            [$($ditem)* {$tag $field}],
            [$($rest)*]
        );
    };

    (@step $Name:ident, [$(#[$meta:meta])*],
     [$($sf:tt)*], [$($eitem:tt)*], [$($ditem:tt)*],
     [$(#[$attr:meta])* option $tag:literal => $field:ident : $ty:ty , $($rest:tt)*]) => {
        pxb_message!(@step $Name, [$(#[$meta])*],
            [$($sf)* $(#[$attr])* pub $field: $ty,],
            [$($eitem)* {? $tag $field}],
            [$($ditem)* {$tag $field}],
            [$($rest)*]
        );
    };

    (@enc $self:ident, $fw:ident, {+ $tag:literal $field:ident}) => {
        WireField::encode_field(&$self.$field, &mut $fw, $tag);
    };
    (@enc $self:ident, $fw:ident, {? $tag:literal $field:ident}) => {
        if !$self.$field.is_opt_zero() {
            WireField::encode_field(&$self.$field, &mut $fw, $tag);
        }
    };

    // Base: no fields left → skip
    (@decode $m:ident, $tag_id:ident, $kind:ident, $fr:ident,) => {
        $fr.skip($kind)?
    };
    // One field → if match, decode; else recurse
    (@decode $m:ident, $tag_id:ident, $kind:ident, $fr:ident,
     {$dtag:literal $field:ident} $($rest:tt)*) => {
        if $tag_id == $dtag {
            $m.$field = WireField::decode_field($kind, $fr)?;
        } else {
            pxb_message!(@decode $m, $tag_id, $kind, $fr, $($rest)*)
        }
    };
}
