module top (input en, input [1:0] s, input [3:0] a, b, output logic [3:0] q, output logic [3:0] y, output logic z);
  always_comb begin
    if (en) q = a;               // want
  end
  always @* begin
    case (s)
      0: y = a;                  // want
      1: y = b;
    endcase
  end
  always_comb begin
    if (en) z = 1;               // want
    else begin
      if (s[0]) z = 0;
    end
  end
endmodule
