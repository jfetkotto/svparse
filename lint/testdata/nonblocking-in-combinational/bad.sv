module top (input [3:0] a, b, output logic [3:0] x, y);
  always_comb begin
    x <= a & b;                  // want
  end
  always @* begin
    if (a[0]) y <= a;            // want
    else y <= b;                 // want
  end
endmodule
